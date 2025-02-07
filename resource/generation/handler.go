package generation

import (
	"bytes"
	"fmt"
	"go/ast"
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

func (c *Client) runHandlerGeneration() error {
	if err := removeGeneratedFiles(c.handlerDestination, Prefix); err != nil {
		return errors.Wrap(err, "removeGeneratedFiles()")
	}

	var (
		errChan = make(chan error)
		wg      sync.WaitGroup
	)
	for _, s := range c.structNames {
		wg.Add(1)
		go func(structName string) {
			if err := c.generateHandlers(structName); err != nil {
				errChan <- err
			}
			wg.Done()
		}(s)
	}

	go func() {
		wg.Wait()
		close(errChan)
	}()

	var handlerErrors error
	for e := range errChan {
		handlerErrors = errors.Join(handlerErrors, e)
	}

	return handlerErrors
}

func (c *Client) generateHandlers(structName string) error {
	generatedType, err := c.parseTypeForHandlerGeneration(structName)
	if err != nil {
		return errors.Wrap(err, "generatedType()")
	}

	generatedHandlers := []*generatedHandler{
		{
			template:    listTemplate,
			handlerType: List,
		},
	}

	if md, ok := c.tableLookup[c.pluralize(structName)]; ok && !md.IsView {
		generatedHandlers = append(generatedHandlers, []*generatedHandler{
			{
				template:    readTemplate,
				handlerType: Read,
			},
			{
				template:    patchTemplate,
				handlerType: Patch,
			},
		}...)
	}

	opts := make(map[HandlerType]map[OptionType]any)
	for handlerType, options := range c.handlerOptions[structName] {
		opts[handlerType] = make(map[OptionType]any)
		for _, option := range options {
			opts[handlerType][option] = struct{}{}
		}
	}

	var handlerData [][]byte
	for _, h := range generatedHandlers {
		if _, skipGeneration := opts[h.handlerType][NoGenerate]; !skipGeneration {
			data, err := c.handlerContent(h, generatedType)
			if err != nil {
				return errors.Wrap(err, "replaceHandlerFileContent()")
			}

			handlerData = append(handlerData, data)
		}
	}

	if len(handlerData) > 0 {
		fileName := generatedFileName(strings.ToLower(c.caser.ToSnake(c.pluralize(generatedType.Name))))
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
			"Source":   c.resourceFilePath,
			"Handlers": string(bytes.Join(handlerData, []byte("\n\n"))),
		}); err != nil {
			return errors.Wrap(err, "tmpl.Execute()")
		}

		log.Printf("Generating handler file: %s", fileName)

		if err := c.writeBytesToFile(destinationFilePath, file, buf.Bytes(), true); err != nil {
			return err
		}
	}

	return nil
}

func (c *Client) handlerContent(handler *generatedHandler, generated *generatedType) ([]byte, error) {
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

func (c *Client) parseTypeForHandlerGeneration(structName string) (*generatedType, error) {
	generatedStruct := &generatedType{IsCompoundTable: true}

declLoop:
	for _, decl := range c.resourceTree.Decls {
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
						field.IsIndex = md.IsIndex
						field.IsUniqueIndex = md.IsUniqueIndex
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

func (c *Client) handlerName(structName string, handlerType HandlerType) string {
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
