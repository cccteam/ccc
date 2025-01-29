package generation

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"text/template"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

func (c *GenerationClient) RunHandlerGeneration() error {
	structs, err := c.structsFromSource()
	if err != nil {
		return errors.Wrap(err, "c.structsFromSource()")
	}

	if err := removeGeneratedFiles(c.handlerDestination, Suffix); err != nil {
		return errors.Wrap(err, "removeGeneratedFiles()")
	}

	var (
		errChan = make(chan error)
		wg      sync.WaitGroup
	)
	// todo(jkyte): There's an issue with imports.Process() causing the generateHandlers() processto run for around 8s for each struct
	for _, s := range structs {
		wg.Add(1)
		go func(structName string) {
			if err := c.generateHandlers(structName); err != nil {
				errChan <- err
			}
			wg.Done()
		}(s)
	}

	var handlerErrors error
	go func() {
		for e := range errChan {
			handlerErrors = errors.Join(handlerErrors, e)
		}
	}()

	wg.Wait()

	return handlerErrors
}

func (c *GenerationClient) removeHandlerDestinationFiles() error {
	dir, err := os.Open(c.handlerDestination)
	if err != nil {
		return errors.Wrap(err, "os.Open()")
	}
	defer dir.Close()

	files, err := dir.Readdirnames(0)
	if err != nil {
		return errors.Wrap(err, "dir.Readdirnames()")
	}

	for _, f := range files {
		if strings.HasSuffix(f, "_generated.go") {
			if err := os.Remove(filepath.Join(c.handlerDestination, f)); err != nil {
				return errors.Wrap(err, "os.Remove()")
			}
		}
	}

	return nil
}

func (c *GenerationClient) generateHandlers(structName string) error {
	generatedType, err := c.parseTypeForHandlerGeneration(structName)
	if err != nil {
		return errors.Wrap(err, "generatedType()")
	}

	generatedHandlers := []*generatedHandler{
		{
			template:    listTemplate,
			handlerType: List,
		},
		{
			template:    readTemplate,
			handlerType: Read,
		},
	}

	if md, ok := c.tableLookup[c.pluralize(structName)]; ok && !md.IsView {
		generatedHandlers = append(generatedHandlers, &generatedHandler{
			template:    patchTemplate,
			handlerType: Patch,
		})
	}

	opts := make(map[HandlerType]map[OptionType]any)
	for handlerType, options := range c.handlerOptions[structName] {
		opts[handlerType] = make(map[OptionType]any)
		for _, option := range options {
			opts[handlerType][option] = struct{}{}
		}
	}

	var handlerData []byte
	for _, h := range generatedHandlers {
		if _, skipGeneration := opts[h.handlerType][NoGenerate]; !skipGeneration {
			data, err := c.handlerContent(h, generatedType)
			if err != nil {
				return errors.Wrap(err, "replaceHandlerFileContent()")
			}

			handlerData = append(handlerData, joinBytes(data, []byte("\n\n"))...)
		}
	}

	if len(handlerData) > 0 {
		fileName := fmt.Sprintf("%s_generated.go", strings.ToLower(c.caser.ToSnake(c.pluralize(generatedType.Name))))
		destinationFilePath := filepath.Join(c.handlerDestination, fileName)

		file, err := os.OpenFile(destinationFilePath, os.O_RDWR|os.O_CREATE, 0o644)
		if err != nil {
			return errors.Wrap(err, "os.OpenFile()")
		}
		defer file.Close()

		tmpl, err := template.New("handlers").Funcs(c.templateFuncs()).Parse(handlerHeaderTemplate)
		if err != nil {
			return errors.Wrap(err, "template.New().Parse()")
		}

		buf := bytes.NewBuffer(nil)
		if err := tmpl.Execute(buf, map[string]any{
			"Source":   c.resourceSource,
			"Handlers": string(handlerData),
		}); err != nil {
			return errors.Wrap(err, "tmpl.Execute()")
		}

		if err := c.writeBytesToFile(destinationFilePath, file, buf.Bytes()); err != nil {
			return err
		}
	}

	return nil
}

func (c *GenerationClient) handlerContent(handler *generatedHandler, generated *generatedType) ([]byte, error) {
	log.Printf("Generating handler: %v\n", c.handlerName(generated.Name, handler.handlerType))

	tmpl, err := template.New("handler").Funcs(c.templateFuncs()).Parse(handler.template)
	if err != nil {
		return nil, errors.Wrap(err, "template.New().Parse()")
	}

	buf := bytes.NewBuffer([]byte{})
	if err := tmpl.Execute(buf, map[string]any{
		"Type": generated,
	}); err != nil {
		return nil, errors.Wrap(err, "tmpl.Execute()")
	}

	return buf.Bytes(), nil
}

func (c *GenerationClient) parseTypeForHandlerGeneration(structName string) (*generatedType, error) {
	tk := token.NewFileSet()
	parse, err := parser.ParseFile(tk, c.resourceSource, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}

	if parse == nil {
		return nil, errors.New("unable to parse file")
	}

	generatedStruct := &generatedType{IsCompoundTable: true}

declLoop:
	for _, decl := range parse.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}

		for _, s := range gd.Specs {
			spec, ok := s.(*ast.TypeSpec)
			if !ok || spec.Name == nil || spec.Name.Name != structName {
				continue
			}
			st, ok := spec.Type.(*ast.StructType)
			if !ok {
				continue
			}
			if st.Fields == nil {
				continue
			}

			table, ok := c.tableLookup[c.pluralize(structName)]
			if !ok {
				return nil, errors.Newf("table not found: %s", c.pluralize(structName))
			}

			var fields []*typeField
			for _, f := range st.Fields.List {
				if len(f.Names) == 0 {
					continue
				}

				field := &typeField{
					Name: f.Names[0].Name,
					Type: fieldType(f.Type, true),
				}

				if f.Tag != nil {
					field.Tag = f.Tag.Value[1 : len(f.Tag.Value)-1]
					structTag := reflect.StructTag(field.Tag)
					parseTags(field, structTag)

					spannerCol := structTag.Get("spanner")
					if md, ok := table.Columns[spannerCol]; ok {
						field.ConstraintType = string(md.ConstraintType)
						field.IsPrimaryKey = md.ConstraintType == PrimaryKey
					}
				}

				if !field.IsPrimaryKey {
					generatedStruct.IsCompoundTable = false
				}

				fields = append(fields, field)
			}

			generatedStruct.IsCompoundTable = generatedStruct.IsCompoundTable == (len(fields) > 1)
			generatedStruct.Name = structName
			generatedStruct.Fields = fields

			break declLoop
		}
	}

	return generatedStruct, nil
}

func parseTags(field *typeField, fieldTag reflect.StructTag) {
	if perms := fieldTag.Get("perm"); perms != "" {
		if strings.Contains(perms, string(accesstypes.Read)) {
			field.ReadPerm = string(accesstypes.Read)
		}
		if strings.Contains(perms, string(accesstypes.List)) {
			field.ListPerm = string(accesstypes.List)
		}

		permList := strings.Split(perms, ",")
		var patchPerms []string
		for _, p := range permList {
			if p == string(accesstypes.Read) || p == string(accesstypes.List) {
				continue
			}

			patchPerms = append(patchPerms, p)
		}
		if len(patchPerms) > 0 {
			field.PatchPerm = strings.Join(patchPerms, ",")
		}
	}

	if query := fieldTag.Get("query"); query != "" {
		field.QueryTag = fmt.Sprintf("query:%q", query)
	}

	if conditions := fieldTag.Get("conditions"); conditions != "" {
		field.Conditions = strings.Split(conditions, ",")
	}
}

func (c *GenerationClient) handlerName(structName string, handlerType HandlerType) string {
	var functionName string
	switch handlerType {
	case List:
		functionName = c.pluralize(structName)
	case Read:
		functionName = structName
	case Patch:
		functionName = "Patch" + c.pluralize(structName)
	}

	return functionName
}

func joinBytes(p ...[]byte) []byte {
	return bytes.Join(p, []byte(""))
}
