package generation

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"text/template"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
)

func (c *GenerationClient) RunHandlerGeneration() error {
	structs, err := c.structsFromSource()
	if err != nil {
		return errors.Wrap(err, "c.structsFromSource()")
	}

	generatedRoutesMap := make(map[string][]generatedRoute)

	for _, s := range structs {
		generatedHandlerTypes, err := c.generateHandlers(s)
		if err != nil {
			return errors.Wrap(err, "c.generateHandlers()")
		}

		for _, ht := range generatedHandlerTypes {
			handler := c.handlerName(s, ht)
			path := fmt.Sprintf("%s/%s", c.routePrefix, c.caser.ToKebab(c.pluralize(s)))
			if ht == Read {
				path = fmt.Sprintf("%s/{%s}", path, strcase.ToGoCamel(s+"ID"))
			}

			var method string
			switch ht {
			case Read, List:
				method = "Get"
			case Patch:
				method = "Patch"
			}

			generatedRoutesMap[s] = append(generatedRoutesMap[s], generatedRoute{
				Method:      method,
				Path:        path,
				HandlerFunc: handler,
			})
		}
	}

	if c.routesDestination != "" {
		if err := c.writeRoutes(generatedRoutesMap); err != nil {
			return errors.Wrap(err, "c.writeRoutes()")
		}
	}

	return nil
}

func (c *GenerationClient) generateHandlers(structName string) ([]HandlerType, error) {
	generatedType, err := c.parseTypeForHandlerGeneration(structName)
	if err != nil {
		return nil, errors.Wrap(err, "generatedType()")
	}

	handlers := []*generatedHandler{
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
		handlers = append(handlers, &generatedHandler{
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

	fileName := fmt.Sprintf("%s.go", strings.ToLower(c.caser.ToSnake(c.pluralize(generatedType.Name))))
	destinationFilePath := filepath.Join(c.handlerDestination, fileName)

	file, err := os.OpenFile(destinationFilePath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, errors.Wrap(err, "os.OpenFile()")
	}
	defer file.Close()

	fileData, err := io.ReadAll(file)
	if err != nil {
		return nil, errors.Wrap(err, "io.ReadAll()")
	}

	if len(fileData) == 0 {
		fileData = []byte("package app\n")
	}

	generatedHandlerTypes := make([]HandlerType, 0)
	for _, h := range handlers {
		functionName := c.handlerName(structName, h.handlerType)

		_, skipGeneration := opts[h.handlerType][NoGenerate]
		fileData, err = c.replaceHandlerFileContent(fileData, functionName, h, generatedType, skipGeneration)
		if err != nil {
			return nil, errors.Wrap(err, "c.replaceHandlerFileContent()")
		}

		if !skipGeneration {
			generatedHandlerTypes = append(generatedHandlerTypes, h.handlerType)
		}
	}

	if len(bytes.TrimPrefix(fileData, []byte("package app\n"))) > 0 {
		if err := c.writeBytesToFile(c.handlerDestination, file, fileData); err != nil {
			return nil, errors.Wrap(err, "c.writeBytesToFile()")
		}
	} else {
		if err := file.Close(); err != nil {
			return nil, errors.Wrap(err, "file.Close()")
		}

		if err := os.Remove(destinationFilePath); err != nil {
			return nil, errors.Wrap(err, "os.Remove()")
		}
	}

	return generatedHandlerTypes, nil
}

func (c *GenerationClient) replaceHandlerFileContent(existingContent []byte, resultFunctionName string, handler *generatedHandler, generated *generatedType, emptyContent bool) ([]byte, error) {
	var newFunctionContent []byte

	if !emptyContent {
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

		newFunctionContent = buf.Bytes()
	}

	newContent, err := c.writeHandler(resultFunctionName, existingContent, newFunctionContent)
	if err != nil {
		return nil, errors.Wrap(err, "replaceFunction()")
	}

	return newContent, nil
}

func (c *GenerationClient) writeHandler(functionName string, existingContent, newFunctionContent []byte) ([]byte, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "", existingContent, parser.AllErrors|parser.SkipObjectResolution)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}

	var start, end token.Pos
	for _, decl := range node.Decls {
		if funcDecl, ok := decl.(*ast.FuncDecl); ok && funcDecl.Name.Name == functionName {
			start = funcDecl.Pos()
			end = funcDecl.End()

			break
		}
	}

	log.Printf("Generating handler: %v\n", functionName)

	if start == token.NoPos || end == token.NoPos {
		return joinBytes(existingContent, []byte("\n\n"), newFunctionContent), nil
	}

	startOffset := fset.Position(start).Offset
	endOffset := fset.Position(end).Offset

	return joinBytes(existingContent[:startOffset], newFunctionContent, existingContent[endOffset:]), nil
}

func (c *GenerationClient) writeRoutes(generatedRoutes map[string][]generatedRoute) error {
	destinationFile := filepath.Join(c.routesDestination, routesFilename)

	file, err := os.OpenFile(destinationFile, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return errors.Wrap(err, "os.OpenFile()")
	}
	defer file.Close()

	tmpl, err := template.New("routes").Funcs(c.templateFuncs()).Parse(routesTemplate)
	if err != nil {
		return errors.Wrap(err, "template.New().Parse()")
	}

	buf := bytes.NewBuffer([]byte{})
	if err := tmpl.Execute(buf, map[string]any{
		"Source":    c.resourceSource,
		"Package":   c.routesDestinationPackage,
		"RoutesMap": generatedRoutes,
	}); err != nil {
		return errors.Wrap(err, "tmpl.Execute()")
	}

	log.Println("Generating route handlers")

	if err := c.writeBytesToFile(destinationFile, file, buf.Bytes()); err != nil {
		return errors.Wrap(err, "c.writeBytesToFile()")
	}

	return nil
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

func (c *GenerationClient) structRoute(structName string, handlerType HandlerType) string {
	handler := c.handlerName(structName, handlerType)
	route := fmt.Sprintf("%s/%s", c.routePrefix, c.caser.ToKebab(c.pluralize(structName)))

	switch handlerType {
	case List:
		return fmt.Sprintf("r.Get(\"%s\", h.%s())", route, handler)
	case Read:
		return fmt.Sprintf("r.Get(\"%s/{%s}\", h.%s())", route, strcase.ToGoCamel(structName+"ID"), handler)
	case Patch:
		return fmt.Sprintf("r.Patch(%q, h.%s())", route, handler)
	}

	return ""
}

func joinBytes(p ...[]byte) []byte {
	return bytes.Join(p, []byte(""))
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
