package generation

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"text/template"

	"github.com/cccteam/ccc/pkg"
	"github.com/cccteam/ccc/resource/generation/graph"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
)

func NewSchemaGenerator(resourceFilePath, schemaDestinationPath, datamigrationPath string, offset int, generateResources bool) (Generator, error) {
	s := &schemaGenerator{
		schemaDestination:    schemaDestinationPath,
		resourceFilePath:     resourceFilePath,
		datamigrationPath:    datamigrationPath,
		generateResources:    generateResources,
		migrationIndexOffset: offset,
	}

	if filepath.Ext(resourceFilePath) == "" {
		s.resourceDestination = resourceFilePath
	} else {
		s.resourceDestination = filepath.Dir(resourceFilePath)
	}

	pkgInfo, err := pkg.Info()
	if err != nil {
		return nil, errors.Wrap(err, "pkg.Info()")
	}
	s.appName = pkgInfo.PackageName

	if err := os.Chdir(pkgInfo.AbsolutePath); err != nil {
		return nil, errors.Wrap(err, "os.Chdir()")
	}

	return s, nil
}

func (s *schemaGenerator) Generate() error {
	if err := removeGeneratedFiles(s.resourceDestination, Prefix); err != nil {
		return err
	}

	resourcePackage, err := parser.LoadPackage(s.resourceFilePath)
	if err != nil {
		return err
	}

	s.packageName = resourcePackage.Name

	pStructs := parser.ParseStructs(resourcePackage)
	schemaInfo, err := newSchema(pStructs)
	if err != nil {
		return err
	}

	if err := s.buildGraph(schemaInfo); err != nil {
		return err
	}

	if err := s.generateSchemaMigrations(schemaInfo); err != nil {
		return err
	}

	if s.generateResources {
		if err := s.generateConversionMethods(schemaInfo); err != nil {
			return err
		}
	}

	if err := s.generateDatamigration(); err != nil {
		return err
	}

	return nil
}

func (s *schemaGenerator) buildGraph(schemaInfo *schema) error {
	if schemaInfo == nil {
		panic("schemaInfo cannot be nil")
	}

	tableMap := make(map[string]*schemaTable, len(schemaInfo.tables))
	for _, table := range schemaInfo.tables {
		tableMap[table.Name] = table
	}

	s.schemaGraph = graph.New[*schemaTable](uint(len(schemaInfo.tables)))

	for _, table := range schemaInfo.tables {
		tableNode := s.schemaGraph.Insert(table)

		for _, foreignKey := range table.ForeignKeys {

			refTable, ok := tableMap[foreignKey.referencedTable]
			if !ok {
				return errors.Newf("table %q references non-existant table %q", table.Name, foreignKey.referencedTable)
			}
			refTableNode := s.schemaGraph.Insert(refTable)
			s.schemaGraph.AddPath(tableNode, refTableNode)
		}
	}

	return nil
}

func (s *schemaGenerator) migrationOrder() []*schemaTable {
	migrationOrder := make([]*schemaTable, 0, s.schemaGraph.Length())

	compareFn := func(a, b *schemaTable) int {
		return strings.Compare(a.Name, b.Name)
	}

	migrationOrder = append(migrationOrder, s.schemaGraph.OrderedList(compareFn)...)

	return migrationOrder
}

func (s *schemaGenerator) generateSchemaMigrations(schemaInfo *schema) error {
	if schemaInfo == nil {
		panic("schemaInfo cannot be nil")
	}

	if err := os.MkdirAll(s.schemaDestination, 0o777); err != nil {
		return errors.Wrap(err, "os.MkdirAll()")
	}

	if err := removeFilesByType(s.schemaDestination, ".sql"); err != nil {
		return err
	}

	var (
		wg      sync.WaitGroup
		errChan = make(chan error)
	)

	migrationIndex := s.migrationIndexOffset
	for _, table := range s.migrationOrder() {
		migrateFunc := func(index int, table *schemaTable, suffix, migrationTemplate string) {
			fileName := sqlMigrationFileName(index, table.Name, suffix)
			if err := s.generateMigration(fileName, migrationTemplate, table); err != nil {
				errChan <- err
			}

			wg.Done()
		}

		wg.Add(1)
		go migrateFunc(migrationIndex, table, migrationSuffixUp, migrationTableUpTemplate)
		wg.Add(1)
		go migrateFunc(migrationIndex, table, migrationSuffixDown, migrationTableDownTemplate)

		migrationIndex += 1
	}

	for _, view := range schemaInfo.views {
		migrateFunc := func(index int, view *schemaView, suffix, templateName string) {
			fileName := sqlMigrationFileName(index, view.Name, suffix)
			if err := s.generateMigration(fileName, templateName, view); err != nil {
				errChan <- err
			}

			wg.Done()
		}

		wg.Add(1)
		go migrateFunc(migrationIndex, view, migrationSuffixUp, migrationViewUpTemplate)
		wg.Add(1)
		go migrateFunc(migrationIndex, view, migrationSuffixDown, migrationViewDownTemplate)

		migrationIndex += 1
	}

	go func() {
		wg.Wait()
		close(errChan)
	}()

	var migrationErrors error
	for e := range errChan {
		migrationErrors = errors.Join(migrationErrors, e)
	}

	if migrationErrors != nil {
		return migrationErrors
	}

	return nil
}

func (s *schemaGenerator) generateConversionMethods(schemaInfo *schema) error {
	if schemaInfo == nil {
		panic("schemaInfo cannot be nil")
	}

	var (
		wg      sync.WaitGroup
		errChan = make(chan error)
	)
	caser := strcase.NewCaser(false, nil, nil)

	for _, table := range schemaInfo.tables {
		conversionFunc := func(table *schemaTable) {
			fileName := fmt.Sprintf("%s_%s.go", genPrefix, strings.ToLower(caser.ToSnake(table.Name)))
			if err := s.generateConversionFile(fileName, table); err != nil {
				errChan <- err
			}

			wg.Done()
		}

		wg.Add(1)
		go conversionFunc(table)
	}

	go func() {
		wg.Wait()
		close(errChan)
	}()

	var conversion error
	for e := range errChan {
		conversion = errors.Join(conversion, e)
	}

	if conversion != nil {
		return conversion
	}

	return nil
}

func (s *schemaGenerator) generateConversionFile(fileName string, table *schemaTable) error {
	data, err := executeTemplate("conversionTemplate", conversionTemplate, map[string]any{
		"HeaderComment": schemaGenHeaderComment,
		"AppName":       s.appName,
		"PackageName":   s.packageName,
		"Resource":      table,
	})
	if err != nil {
		return err
	}

	destinationFilePath := filepath.Join(s.resourceDestination, fileName)

	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	formattedBytes, err := s.goFormatBytes(file.Name(), data)
	if err != nil {
		return err
	}

	if err := s.writeBytesToFile(file, formattedBytes); err != nil {
		return err
	}

	return nil
}

func (s *schemaGenerator) generateDatamigration() error {
	order := s.migrationOrder()
	tableMap := make(map[*schemaTable][]*schemaTable, len(order))
	compareFn := func(a, b *schemaTable) int {
		return strings.Compare(a.Name, b.Name)
	}

	for _, table := range order {
		tableDeps := s.schemaGraph.Get(table).Dependencies()
		slices.SortFunc(tableDeps, compareFn)
		tableMap[table] = tableDeps
	}

	data, err := executeTemplate("datamigrationTemplate", datamigrationTemplate, map[string]any{
		"HeaderComment": schemaGenHeaderComment,
		"AppName":       s.appName,
		"PackageName":   "datamigration",
		"Tables":        order,
		"TableMap":      tableMap,
	})
	if err != nil {
		return err
	}

	destinationFilePath := filepath.Join(s.datamigrationPath, genPrefix+"_datamigration.go")

	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	formattedBytes, err := s.goFormatBytes(file.Name(), data)
	if err != nil {
		return err
	}

	if err := s.writeBytesToFile(file, formattedBytes); err != nil {
		return err
	}

	return nil
}

func (*schemaGenerator) Close() {}

func sqlMigrationFileName(migrationIndex int, tableName, suffix string) string {
	return fmt.Sprintf("%06d_%s.%s", migrationIndex, tableName, suffix)
}

func (s *schemaGenerator) generateMigration(fileName, migrationTemplate string, schemaResource any) error {
	data, err := executeTemplate("migrationTemplate", migrationTemplate, map[string]any{
		"Resource":               schemaResource,
		"MigrationHeaderComment": schemaGenHeaderComment,
	})
	if err != nil {
		return err
	}

	destinationFilePath := filepath.Join(s.schemaDestination, fileName)

	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := s.writeBytesToFile(file, data); err != nil {
		return err
	}

	return nil
}

func executeTemplate(templateName, templateSrc string, data map[string]any) ([]byte, error) {
	tmpl, err := template.New(templateName).Parse(templateSrc)
	if err != nil {
		return nil, errors.Wrap(err, "template.Parse()")
	}

	buf := bytes.NewBuffer([]byte{})
	if err := tmpl.Execute(buf, data); err != nil {
		return nil, errors.Wrap(err, "tmpl.Execute()")
	}

	return buf.Bytes(), nil
}

func newSchema(pStructs []*parser.Struct) (*schema, error) {
	s := &schema{
		tables: make([]*schemaTable, 0, len(pStructs)),
		views:  make([]*schemaView, 0, len(pStructs)),
	}

	for i := range pStructs {
		structComments, err := genlang.ScanStruct(pStructs[i].Comments())
		if err != nil {
			return nil, errors.Wrapf(err, "%s commentlang.Scan()", pStructs[i].Error())
		}

		_, isView := structComments[genlang.View]
		if isView {
			view, err := newSchemaView(pStructs[i])
			if err != nil {
				return nil, err
			}

			if err := view.resolveStructComments(structComments); err != nil {
				return nil, err
			}

			s.views = append(s.views, view)
		} else {
			table, err := newSchemaTable(pStructs[i])
			if err != nil {
				return nil, err
			}

			if err := table.resolveStructComments(structComments); err != nil {
				return nil, err
			}

			s.tables = append(s.tables, table)
		}
	}

	s.tables = slices.Clip(s.tables)
	s.views = slices.Clip(s.views)

	return s, nil
}

func newSchemaTable(pStruct *parser.Struct) (*schemaTable, error) {
	table := &schemaTable{
		Name:             pStruct.Name(),
		Columns:          make([]*tableColumn, 0, pStruct.NumFields()),
		HasConvertMethod: pStruct.HasMethod("Convert"),
		HasFilterMethod:  pStruct.HasMethod("Filter"),
	}

	for _, field := range pStruct.Fields() {
		fieldComments, err := genlang.ScanField(field.Comments())
		if err != nil {
			return nil, errors.Wrap(err, "commentlang.ScanField()")
		}

		// Some columns need to be scanned in from the DB but are concatenated with other columns,
		// then we don't want them in the output.
		if _, ok := fieldComments[genlang.Suppress]; ok {
			continue
		}

		col := tableColumn{
			Table:      table,
			Name:       strings.ReplaceAll(field.Name(), "ID", "Id"),
			IsNullable: isSQLTypeNullable(field),
			SQLType:    decodeSQLType(field),
		}

		if pStruct.HasMethod(field.Name() + "Conversion") {
			col.conversionMethod = custom
		} else {
			col.conversionMethod = determineConversionMethod(field)
		}

		col, err = table.resolveFieldComment(col, fieldComments)
		if err != nil {
			return nil, err
		}

		table.Columns = append(table.Columns, &col)
	}

	return table, nil
}

func newSchemaView(pStruct *parser.Struct) (*schemaView, error) {
	view := &schemaView{
		Name:    pStruct.Name(),
		Columns: make([]*viewColumn, 0, pStruct.NumFields()),
	}

	for _, field := range pStruct.Fields() {
		col, err := newViewColumn(field)
		if err != nil {
			return nil, err
		}

		view.Columns = append(view.Columns, &col)
	}

	return view, nil
}

func isSQLTypeNullable(f *parser.Field) bool {
	if decodeSQLType(f) == "BOOL" {
		return false
	}

	if f.IsPointer() {
		return true
	}

	return strings.Contains(strings.ToLower(f.UnqualifiedTypeName()), "null")
}

func decodeSQLType(f *parser.Field) string {
	tt := f.TypeName()

	if f.TypeArgs() != "" {
		tt = f.TypeArgs()
	}

	switch tt {
	case "string":
		return "STRING(MAX)"
	case "bool":
		return "BOOL"
	case "ccc.UUID", "UUID", "ccc.NullUUID":
		return "STRING(36)"
	case "int", "int64":
		return "INT64"
	case "float":
		return "FLOAT64"
	case "civil.Date", "Date":
		return "DATE"
	case "time.Time", "sql.NullTime":
		return "TIMESTAMP"
	case "Decimal":
		return "NUMERIC"
	default:
		panic(fmt.Sprintf("schemagen SQL type unimplemented for type=%q (%s)", f.Type(), tt))
	}
}

func removeFilesByType(directory, fileExtension string) error {
	dir, err := os.Open(directory)
	if err != nil {
		return errors.Wrap(err, "os.Open()")
	}

	files, err := dir.Readdirnames(0)
	if err != nil {
		return errors.Wrap(err, "os.File.Readdirnames()")
	}

	if err := dir.Close(); err != nil {
		return errors.Wrap(err, "os.File.Close()")
	}

	for _, f := range files {
		if !strings.HasSuffix(f, fileExtension) {
			continue
		}

		fp := filepath.Join(directory, f)
		if err := os.Remove(fp); err != nil {
			return errors.Wrap(err, "os.Remove()")
		}
	}

	return nil
}

func determineConversionMethod(field *parser.Field) conversionFlag {
	var flag conversionFlag
	typeArgs := field.TypeArgs()
	if typeArgs == "" {
		return flag
	}

	if field.IsPointer() {
		flag |= pointer
	}

	tt := field.OriginType()

	switch tt {
	case "IntTo":
		flag |= fromInt
	case "StringTo":
		flag |= fromString
	default:
		panic(fmt.Sprintf("schemagen convert-from unimplemented for type=%q", tt))
	}

	switch typeArgs {
	case "int", "int64":
		flag |= toInt
	case "string":
		flag |= toString
	case "bool":
		flag |= toBool
	case "UUID":
		flag |= toUUID
	case "Decimal":
		flag = noConversion
	default:
		panic(fmt.Sprintf("schemagen convert-to unimplemented for type=%q", typeArgs))
	}

	return flag
}
