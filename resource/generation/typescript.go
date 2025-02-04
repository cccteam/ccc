package generation

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/template"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

func (c *GenerationClient) runTypescriptPermissionGeneration() error {
	templateData := c.rc.TypescriptData()

	if err := removeGeneratedFiles(c.typescriptDestination, HeaderComment); err != nil {
		return errors.Wrap(err, "removeGeneratedFiles()")
	}

	output, err := c.generateTemplateOutput(typescriptPermissionTemplate, map[string]any{
		"Header":              typescriptTemplateHeader,
		"Permissions":         templateData.Permissions,
		"Resources":           templateData.Resources,
		"ResourceTags":        templateData.ResourceTags,
		"ResourcePermissions": templateData.ResourcePermissions,
		"Domains":             templateData.Domains,
		"Metadata":            c.metadataTemplate,
	})
	if err != nil {
		return errors.Wrap(err, "c.generateTemplateOutput()")
	}
	destinationFilePath := filepath.Join(c.typescriptDestination, "resources.ts")

	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := c.writeBytesToFile(destinationFilePath, file, output, false); err != nil {
		return errors.Wrap(err, "c.writeBytesToFile()")
	}

	log.Printf("Generated Permissions: %s\n", file.Name())

	return nil
}

func (c *GenerationClient) runTypescriptMetadataGeneration() error {
	if c.genTypescriptPerm == nil {
		if err := removeGeneratedFiles(c.typescriptDestination, HeaderComment); err != nil {
			return errors.Wrap(err, "removeGeneratedFiles()")
		}
	}

	if err := c.generateTypescriptMetadata(); err != nil {
		return errors.Wrap(err, "generateTypescriptResources")
	}

	return nil
}

func (c *GenerationClient) generateTypescriptMetadata() error {
	routerResources := c.rc.Resources()
	structNames, err := c.structsFromSource()
	if err != nil {
		return errors.Wrap(err, "c.structsFromSource()")
	}

	var genResources []*generatedResource
	for _, s := range structNames {
		// We only want to generate metadata for Resources that are registered in the Router
		if slices.Contains(routerResources, accesstypes.Resource(c.pluralize(s))) {
			genResource, err := c.parseStructForTypescriptGeneration(s)
			if err != nil {
				return errors.Wrap(err, "generatedType()")
			}

			genResources = append(genResources, genResource)
		}
	}

	var header string
	if c.genTypescriptPerm == nil {
		header = typescriptTemplateHeader
	}

	output, err := c.generateTemplateOutput(typescriptMetadataTemplate, map[string]any{
		"Resources": genResources,
		"Header":    header,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	if c.genTypescriptPerm != nil {
		c.metadataTemplate = output

		return nil
	}

	destinationFilePath := filepath.Join(c.typescriptDestination, "resources.ts")

	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := c.writeBytesToFile(destinationFilePath, file, output, false); err != nil {
		return errors.Wrap(err, "c.writeBytesToFile()")
	}

	return nil
}

func (c *GenerationClient) parseStructForTypescriptGeneration(structName string) (*generatedResource, error) {
	tk := token.NewFileSet()
	parse, err := parser.ParseFile(tk, c.resourceSource, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}

	if parse == nil {
		return nil, errors.New("unable to parse file")
	}

	resource := &generatedResource{Name: structName}

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

			_, ok = c.tableLookup[c.pluralize(structName)]
			if !ok {
				return nil, errors.Newf("table not found: %s", c.pluralize(structName))
			}

			var fields []*generatedResource
			for _, f := range st.Fields.List {

				if len(f.Names) == 0 {
					continue
				}

				field := &generatedResource{
					Name:     f.Names[0].Name,
					DataType: typescriptType(f.Type),
				}

				fields = append(fields, field)
			}

			resource.Fields = fields

			break declLoop
		}
	}

	return resource, nil
}

func typescriptType(t ast.Expr) string {
	switch t := t.(type) {
	case *ast.Ident:
		switch {
		case strings.Contains(t.Name, "bool"):
			return "boolean"
		case strings.Contains(t.Name, "string"), strings.Contains(t.Name, "UUID"):
			return "string"
		case strings.Contains(t.Name, "int"), strings.Contains(t.Name, "float"), strings.Contains(t.Name, "Decimal"):
			return "number"
		case strings.Contains(t.Name, "Time"):
			return "Date"
		default:
			log.Panicf("type `%s` is not supported (yet)", t.Name)
			return "todo"
		}
	case *ast.SelectorExpr:
		return typescriptType(t.Sel)
	case *ast.StarExpr:
		return typescriptType(t.X)
	default:
		return "todo"
	}
}

func (c *GenerationClient) generateTypescriptTemplate(fileTemplate string, data map[string]any) ([]byte, error) {
	tmpl, err := template.New(fileTemplate).Funcs(c.templateFuncs()).Parse(fileTemplate)
	if err != nil {
		return nil, errors.Wrap(err, "template.Parse()")
	}

	buf := bytes.NewBuffer([]byte{})
	if err := tmpl.Execute(buf, data); err != nil {
		return nil, errors.Wrap(err, "tmpl.Execute()")
	}

	return buf.Bytes(), nil
}
