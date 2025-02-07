package generation

import (
	"bytes"
	"go/ast"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"text/template"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

func (c *Client) runTypescriptPermissionGeneration() error {
	templateData := c.rc.TypescriptData()

	if c.genTypescriptMeta == nil {
		if err := removeGeneratedFiles(c.typescriptDestination, HeaderComment); err != nil {
			return errors.Wrap(err, "removeGeneratedFiles()")
		}
	}

	output, err := c.generateTemplateOutput(typescriptPermissionTemplate, map[string]any{
		"Permissions":         templateData.Permissions,
		"Resources":           templateData.Resources,
		"ResourceTags":        templateData.ResourceTags,
		"ResourcePermissions": templateData.ResourcePermissions,
		"Domains":             templateData.Domains,
	})
	if err != nil {
		return errors.Wrap(err, "c.generateTemplateOutput()")
	}

	destinationFilePath := filepath.Join(c.typescriptDestination, "resourcePermissions.ts")
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

func (c *Client) runTypescriptMetadataGeneration() error {
	if err := removeGeneratedFiles(c.typescriptDestination, HeaderComment); err != nil {
		return errors.Wrap(err, "removeGeneratedFiles()")
	}

	if err := c.generateTypescriptMetadata(); err != nil {
		return errors.Wrap(err, "generateTypescriptResources")
	}

	return nil
}

func (c *Client) generateTypescriptMetadata() error {
	routerResources := c.rc.Resources()

	var genResources []*generatedResource
	for _, s := range c.structNames {
		// We only want to generate metadata for Resources that are registered in the Router
		if slices.Contains(routerResources, accesstypes.Resource(c.pluralize(s))) {
			genResource, err := c.parseStructForTypescriptGeneration(s)
			if err != nil {
				return errors.Wrap(err, "generatedType()")
			}

			genResources = append(genResources, genResource)
		}
	}

	output, err := c.generateTemplateOutput(typescriptMetadataTemplate, map[string]any{
		"Resources": genResources,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
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

	log.Printf("Generated Resource Metadata: %s\n", file.Name())

	return nil
}

func (c *Client) parseStructForTypescriptGeneration(structName string) (*generatedResource, error) {
	resource := &generatedResource{Name: structName}

declLoop:
	for _, decl := range c.resourceTree.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}

		for _, s := range gd.Specs {
			spec, ok := s.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if spec.Name == nil {
				return nil, errors.Newf("parseStructForTypescriptGeneration: reflected struct has no identifier %s", structName)
			}
			if spec.Name.Name != structName {
				continue
			}

			st, ok := spec.Type.(*ast.StructType)
			if !ok {
				return nil, errors.Newf("parseStructForTypescriptGeneration: could not reflect structtype for struct `%s`", structName)
			}
			if st.Fields == nil {
				return nil, errors.Newf("parseStructForTypescriptGeneration: reflected structtype %s has no fields", structName)
			}

			tableMeta, ok := c.tableLookup[c.pluralize(structName)]
			if !ok {
				return nil, errors.Newf("table not found: %s", c.pluralize(structName))
			}

			var fields []*generatedResource
			for _, astField := range st.Fields.List {
				if len(astField.Names) == 0 {
					return nil, errors.Newf("parseStructForTypescriptGeneration: astField has no identifier in struct %s", structName)
				}
				var tag string
				if astField.Tag != nil {
					tag = astField.Tag.Value
				}
				if tag == "" {
					return nil, errors.Newf("parseStructForTypescriptGeneration: astField.Tag is empty in %s", structName)
				}

				column := reflect.StructTag(strings.Trim(tag, "`")).Get("spanner")
				if column == "" {
					return nil, errors.Newf("parseStructForTypescriptGeneration: could not get spanner value from tag %s in struct %s", tag, structName)
				}

				field := &generatedResource{Name: column}
				fieldMeta, ok := tableMeta.Columns[column]
				if !ok {
					return nil, errors.Newf("parseStructForTypescriptGeneration: fieldMeta returned no info for column %s in struct %s", column, structName)
				}
				dataType, err := typescriptType(astField.Type)
				if err != nil {
					return nil, err
				}
				field.dataType = dataType
				field.Required = !fieldMeta.IsNullable

				if fieldMeta.IsForeignKey {
					field.IsForeignKey = true
					field.dataType = "enumerated"
					field.ReferencedResource = fieldMeta.ReferencedTable
					field.ReferencedColumn = fieldMeta.ReferencedColumn
				}

				fields = append(fields, field)
			}

			resource.Fields = fields

			break declLoop
		}
	}

	return resource, nil
}

func typescriptType(t ast.Expr) (string, error) {
	switch t := t.(type) {
	case *ast.Ident:
		switch {
		case t.Name == "Link", t.Name == "NullLink":
			return "link", nil
		case t.Name == "UUID", t.Name == "NullUUID":
			return "uuid", nil
		case t.Name == "bool":
			return "boolean", nil
		case t.Name == "string":
			return "string", nil
		case strings.HasPrefix(t.Name, "int"), strings.HasPrefix(t.Name, "uint"),
			strings.HasPrefix(t.Name, "float"), t.Name == "Decimal", t.Name == "NullDecimal":
			return "number", nil
		case t.Name == "Time", t.Name == "NullTime":
			return "Date", nil
		default:
			return "", errors.Newf("typescriptType: unhandled type `%s`", t.Name)
		}
	case *ast.SelectorExpr:
		return typescriptType(t.Sel)
	case *ast.StarExpr:
		return typescriptType(t.X)
	default:
		return "", errors.Newf("typescriptType: unhandled type at field[%d]", t.Pos())
	}
}

func (c *Client) generateTypescriptTemplate(fileTemplate string, data map[string]any) ([]byte, error) {
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
