package generation

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"text/template"

	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
)

func (c *GenerationClient) RunSpannerGeneration() error {
	if err := c.removeDestinationFiles(); err != nil {
		return errors.Wrap(err, "c.removeDestinationFiles()")
	}

	types, err := c.buildPatcherTypesFromSource()
	if err != nil {
		return errors.Wrap(err, "c.buildPatcherTypesFromSource()")
	}

	if err := c.generateResourceInterfaces(types); err != nil {
		return errors.Wrap(err, "c.generateResourceInterfaces()")
	}

	for _, t := range types {
		if err := c.generatePatcherTypes(t); err != nil {
			return errors.Wrap(err, "c.generatePatcherTypes()")
		}
	}

	return nil
}

func (c *GenerationClient) generateResourceInterfaces(types []*generatedType) error {
	output, err := c.generateTemplateOutput(resourcesInterfaceTemplate, map[string]any{"Source": c.resourceSource, "Types": types})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	destinationFile := filepath.Join(c.spannerDestination, resourceInterfaceOutputFilename)

	file, err := os.Create(destinationFile)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := c.writeBytesToFile(destinationFile, file, output); err != nil {
		return errors.Wrap(err, "c.writeBytesToFile()")
	}

	return nil
}

func (c *GenerationClient) generatePatcherTypes(generatedType *generatedType) error {
	destinationFile := filepath.Join(c.spannerDestination, fmt.Sprintf("%s.go", strcase.ToSnake(c.pluralizer.Plural(generatedType.Name))))
	fmt.Printf("Generating file: %v\n", destinationFile)

	output, err := c.generateTemplateOutput(resourceFileTemplate, map[string]any{
		"Source":          c.resourceSource,
		"Name":            generatedType.Name,
		"IsView":          generatedType.IsView,
		"Fields":          generatedType.Fields,
		"IsCompoundTable": generatedType.IsCompoundTable,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	file, err := os.Create(destinationFile)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := c.writeBytesToFile(destinationFile, file, output); err != nil {
		return errors.Wrap(err, "c.writeBytesToFile()")
	}

	return nil
}

func (c *GenerationClient) removeDestinationFiles() error {
	dir, err := os.Open(c.spannerDestination)
	if err != nil {
		return errors.Wrap(err, "os.Open()")
	}
	defer dir.Close()

	files, err := dir.Readdirnames(0)
	if err != nil {
		return errors.Wrap(err, "dir.Readdirnames()")
	}

	for _, f := range files {
		if f == c.resourceSource {
			continue
		}

		if err := os.Remove(filepath.Join(c.spannerDestination, f)); err != nil {
			return errors.Wrap(err, "os.Remove()")
		}
	}

	return nil
}

func (c *GenerationClient) buildPatcherTypesFromSource() ([]*generatedType, error) {
	tk := token.NewFileSet()
	parse, err := parser.ParseFile(tk, c.resourceSource, nil, 0)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}

	if parse == nil || parse.Scope == nil {
		return nil, errors.New("unable to parse file")
	}

	typeList := make([]*generatedType, 0)

	for k, v := range parse.Scope.Objects {
		var fields []*typeField

		spec, ok := v.Decl.(*ast.TypeSpec)
		if !ok {
			continue
		}
		structType, ok := spec.Type.(*ast.StructType)
		if !ok {
			continue
		}
		if structType.Fields == nil {
			continue
		}

		isCompoundTable := true

		tableName := c.pluralizer.Plural(k)
		for _, f := range structType.Fields.List {
			if len(f.Names) == 0 {
				continue
			}

			field := &typeField{
				Name: f.Names[0].Name,
			}

			field.Type = fieldType(f.Type)

			if f.Tag != nil {
				field.Tag = f.Tag.Value
			}

			if table, ok := c.tableFieldLookup[tableName]; ok {
				if field.Tag != "" {
					structTag := reflect.StructTag(field.Tag[1 : len(field.Tag)-1])
					column := structTag.Get("spanner")

					if data, ok := table.Columns[column]; ok {
						field.IsPrimaryKey = data.ConstraintType == PrimaryKey
						field.IsIndex = data.IsIndex

						if data.ConstraintType != PrimaryKey && data.ConstraintType != ForeignKey {
							isCompoundTable = false
						}
					}
				}
			}

			fields = append(fields, field)
		}

		typeList = append(typeList, &generatedType{
			Name:            k,
			Fields:          fields,
			IsCompoundTable: isCompoundTable,
		})
	}

	sort.Slice(typeList, func(i, j int) bool {
		return typeList[i].Name < typeList[j].Name
	})

	return typeList, nil
}

func (c *GenerationClient) generateTemplateOutput(fileTemplate string, data map[string]any) ([]byte, error) {
	tmpl, err := template.New(fileTemplate).Funcs(templateFuncs).Parse(fileTemplate)
	if err != nil {
		return nil, errors.Wrap(err, "template.Parse()")
	}

	buf := bytes.NewBuffer([]byte{})
	if err := tmpl.Execute(buf, data); err != nil {
		return nil, errors.Wrap(err, "tmpl.Execute()")
	}

	return buf.Bytes(), nil
}
