package generation

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"text/template"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

func (c *GenerationClient) RunHandlerGeneration() error {
	structs, err := c.structsFromSource()
	if err != nil {
		return errors.Wrap(err, "c.structsFromSource()")
	}

	for _, s := range structs {
		if err := c.generateHandlers(s); err != nil {
			return errors.Wrap(err, "c.generateHandlers()")
		}
	}

	return nil
}

func (c *GenerationClient) generateHandlers(structName string) error {
	generatedType, err := c.parseTypeForHandlerGeneration(structName)
	if err != nil {
		return errors.Wrap(err, "generatedType()")
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

	if md, ok := c.tableFieldLookup[structName]; ok && !md.IsView {
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

	outputFile := fmt.Sprintf("%s.go", strings.ToLower(c.caser.ToSnake(c.pluralize(structName))))
	if fileName, ok := c.outputFileOverrides[structName]; ok {
		outputFile = fmt.Sprintf("%s.go", fileName)
	}

	destinationFile := filepath.Join(c.handlerDestination, outputFile)

	file, err := os.OpenFile(destinationFile, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return errors.Wrap(err, "os.OpenFile()")
	}
	defer file.Close()

	fileData, err := io.ReadAll(file)
	if err != nil {
		return errors.Wrap(err, "io.ReadAll()")
	}

	if len(fileData) == 0 {
		fileData = []byte("package app\n")
	}

	for _, h := range handlers {
		functionName := c.handlerName(structName, h.handlerType)
		if _, skipGeneration := opts[h.handlerType][NoGenerate]; !skipGeneration {
			fileData, err = c.replaceHandlerFileContent(fileData, functionName, h, generatedType)
			if err != nil {
				return err
			}
		}
	}

	if len(bytes.TrimPrefix(fileData, []byte("package app\n"))) > 0 {
		if err := c.writeBytesToFile(c.handlerDestination, file, fileData); err != nil {
			return err
		}
	} else {
		if err := file.Close(); err != nil {
			return errors.Wrap(err, "file.Close()")
		}

		if err := os.Remove(destinationFile); err != nil {
			return errors.Wrap(err, "os.Remove()")
		}
	}

	return nil
}

func (c *GenerationClient) replaceHandlerFileContent(existingContent []byte, resultFunctionName string, handler *generatedHandler, generated *generatedType) ([]byte, error) {
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

	newContent, err := c.writeHandler(resultFunctionName, existingContent, buf.Bytes())
	if err != nil {
		return nil, errors.Wrap(err, "replaceFunction()")
	}

	return newContent, nil
}

func (c *GenerationClient) writeHandler(functionName string, existingContent, newFunctionContent []byte) ([]byte, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "", existingContent, parser.AllErrors)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}

	var start, end token.Pos
	for _, decl := range node.Decls {
		if funcDecl, ok := decl.(*ast.FuncDecl); ok {
			if funcDecl.Name.Name == functionName {
				start = funcDecl.Pos()
				end = funcDecl.End()

				break
			}
		}
	}

	if start == token.NoPos || end == token.NoPos {
		fmt.Printf("Generating handler:  %v\n", functionName)

		return joinBytes(existingContent, []byte("\n\n"), newFunctionContent), nil
	}

	fmt.Printf("Regenerating handler: %v\n", functionName)

	startOffset := fset.Position(start).Offset
	endOffset := fset.Position(end).Offset

	return joinBytes(existingContent[:startOffset], newFunctionContent, existingContent[endOffset:]), nil
}

func (c *GenerationClient) parseTypeForHandlerGeneration(structName string) (*generatedType, error) {
	tk := token.NewFileSet()
	parse, err := parser.ParseFile(tk, c.resourceSource, nil, 0)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}

	if parse == nil || parse.Scope == nil {
		return nil, errors.New("unable to parse file")
	}

	generatedStruct := &generatedType{IsCompoundTable: true}
	for _, v := range parse.Scope.Objects {
		var fields []*typeField

		spec, ok := v.Decl.(*ast.TypeSpec)
		if !ok || spec.Name == nil || spec.Name.Name != structName {
			continue
		}
		structType, ok := spec.Type.(*ast.StructType)
		if !ok {
			continue
		}
		if structType.Fields == nil {
			continue
		}

		table, ok := c.tableFieldLookup[c.pluralize(structName)]
		if !ok {
			return nil, errors.Newf("table not found: %s", c.pluralize(structName))
		}

		for _, f := range structType.Fields.List {
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

		generatedStruct.Name = structName
		generatedStruct.Fields = fields

		break
	}

	return generatedStruct, nil
}

func parseTags(field *typeField, fieldTag reflect.StructTag) {
	perms := fieldTag.Get("perm")
	if perms != "" {
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

	query := fieldTag.Get("query")
	if query != "" {
		field.QueryTag = fmt.Sprintf("query:%q", query)
	}

	conditions := fieldTag.Get("conditions")
	if conditions != "" {
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
