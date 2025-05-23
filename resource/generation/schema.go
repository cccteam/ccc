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

	"github.com/cccteam/ccc/resource/generation/dependencygraph"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/commentlang"
	"github.com/go-playground/errors/v5"
)

func NewSchemaGenerator(resourceFilePath, schemaDestinationPath string) (Generator, error) {
	s := &schemaGenerator{
		resourceDestination: filepath.Dir(resourceFilePath),
		schemaDestination:   schemaDestinationPath,
		loadPackages:        []string{resourceFilePath},
	}

	return s, nil
}

func (s *schemaGenerator) Generate() error {
	packageMap, err := parser.LoadPackages(s.loadPackages...)
	if err != nil {
		return err
	}

	pStructs := parser.ParseStructs(packageMap["resources"])

	schemaInfo, err := newSchema(pStructs)
	if err != nil {
		return err
	}

	err = os.MkdirAll(s.schemaDestination, 0o777)
	if err != nil {
		return errors.Wrap(err, "os.MkdirAll()")
	}

	if err := s.generateSchemaMigrations(schemaInfo); err != nil {
		return err
	}

	if err := s.generateConversionMethods(); err != nil {
		return err
	}

	if err := s.generateMutations(); err != nil {
		return err
	}

	return nil
}

func (s *schemaGenerator) generateSchemaMigrations(schemaInfo *schema) error {
	if schemaInfo == nil {
		panic("schemaInfo cannot be nil")
	}

	tableMap := make(map[string]*schemaTable, len(schemaInfo.tables))

	dg := dependencygraph.New()

	migrationOrder := make([]*schemaTable, 0, len(schemaInfo.tables))
	for _, table := range schemaInfo.tables {

		if len(table.ForeignKeys) == 0 {
			migrationOrder = append(migrationOrder, table)

			continue
		}

		tableMap[table.Name] = table

		for _, foreignKey := range table.ForeignKeys {
			if err := dg.AddEdge(table.Name, foreignKey.referencedTable); err != nil {
				return errors.Wrap(err, "dependencygraph.Graph.AddEdge()")
			}
		}
	}

	for _, tableName := range dg.OrderedList() {
		if table, ok := tableMap[tableName]; ok {
			migrationOrder = append(migrationOrder, table)
		}
	}

	// TODO: validate that referenced table names by foreign keys and views are actually in the schema

	var (
		wg      sync.WaitGroup
		errChan = make(chan error)
	)

	migrationIndex := 0
	for _, table := range migrationOrder {
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

func (s *schemaGenerator) generateConversionMethods() error {
	// TODO: determine which fields need to be transformed to fit new schema using astInfo in parser.Field
	return nil
}

func (s *schemaGenerator) generateMutations() error {
	// TODO: generate a spanner insert mutation for each table using the schema definition
	return nil
}

func (schemaGenerator) Close() {}

func sqlMigrationFileName(migrationIndex int, tableName, suffix string) string {
	return fmt.Sprintf("%0000d_%s.%s", migrationIndex, tableName, suffix)
}

func (s *schemaGenerator) generateMigration(fileName, migrationTemplate string, schemaResource any) error {
	data, err := executeMigrationTemplate(migrationTemplate, map[string]any{"Resource": schemaResource})
	if err != nil {
		return err
	}

	destinationFilePath := filepath.Join(s.schemaDestination, fileName)

	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := file.Truncate(0); err != nil {
		return errors.Wrapf(err, "file.Truncate(): file: %s", file.Name())
	}
	if _, err := file.Seek(0, 0); err != nil {
		return errors.Wrapf(err, "file.Seek(): file: %s", file.Name())
	}
	if _, err := file.Write(data); err != nil {
		return errors.Wrapf(err, "file.Write(): file: %s", file.Name())
	}

	return nil
}

func executeMigrationTemplate(migrationTemplate string, data map[string]any) ([]byte, error) {
	data["MigrationHeaderComment"] = migrationHeaderComment
	tmpl, err := template.New("migrationTemplate").Parse(migrationTemplate)
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
		structComments, err := commentlang.ScanStruct(pStructs[i].Comments())
		if err != nil {
			return nil, errors.Wrapf(err, "%s commentlang.Scan()", pStructs[i].Error())
		}

		_, isView := structComments[commentlang.View]
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
		Name:    pStruct.Name(),
		Columns: make([]*tableColumn, 0, pStruct.NumFields()),
	}

	for _, field := range pStruct.Fields() {
		col := tableColumn{
			Name:       strings.ReplaceAll(field.Name(), "ID", "Id"),
			IsNullable: isSQLTypeNullable(field),
			SQLType:    decodeSQLType(field),
		}

		fieldComments, err := commentlang.ScanField(field.Comments())
		if err != nil {
			return nil, errors.Wrap(err, "commentlang.ScanField()")
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
	case "ccc.UUID", "UUID":
		return "STRING(36)"
	case "int":
		return "INT64"
	case "float":
		return "FLOAT64"
	case "civil.Date", "Date":
		return "DATE"
	default:
		panic(fmt.Sprintf("schemagen conversion unimplemented for type=%q", f.Type()))
	}
}
