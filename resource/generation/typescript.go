package generation

import (
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

func (c *GenerationClient) RunTypescriptGeneration() error {
	if err := removeGeneratedFiles(c.typescriptDestination, HeaderComment); err != nil {
		return errors.Wrap(err, "removeGeneratedFiles()")
	}

	log.Println("Generating resources.ts file")

	if err := c.generateTypescriptResources(); err != nil {
		return errors.Wrap(err, "generateTypescriptResources")
	}

	return nil
}

func (c *GenerationClient) generateTypescriptResources() error {
	structs, err := c.structsFromSource()
	if err != nil {
		return errors.Wrap(err, "c.structsFromSource()")
	}

	var genResources []*generatedResource
	for _, s := range structs {
		genResource, err := c.parseStructForTypescriptGeneration(s)
		if err != nil {
			return errors.Wrap(err, "generatedType()")
		}

		genResources = append(genResources, genResource)
	}

	output, err := c.generateTemplateOutput(newTypescriptTemplate, map[string]any{
		"Resources": genResources,
	})

	destinationFilePath := filepath.Join(c.typescriptDestination, "resources2.ts")

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

				if f.Tag != nil {
					tag := f.Tag.Value[1 : len(f.Tag.Value)-1]
					structTag := reflect.StructTag(tag)

					if perm := structTag.Get("perm"); perm != "" {
						addPermToResource(field, perm)
						addPermToResource(resource, perm)
					}
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
		case strings.Contains(t.Name, "int"), strings.Contains(t.Name, "float"):
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

func parsePermTag(perm string) accesstypes.Permission {
	switch perm {
	case string(accesstypes.Create):
		return accesstypes.Create
	case string(accesstypes.Read):
		return accesstypes.Read
	case string(accesstypes.List):
		return accesstypes.List
	case string(accesstypes.Update):
		return accesstypes.Update
	case string(accesstypes.Delete):
		return accesstypes.Delete
	default:
		log.Fatalf("unspoorted perm: %s", perm)
		return accesstypes.Create
	}
}

func addPermToResource(resource *generatedResource, perm string) {
	if strings.Contains(perm, ",") {
		permList := strings.Split(perm, ",")
		for _, perm := range permList {
			addPermToResource(resource, perm)
		}
	} else {
		if !slices.Contains(resource.Permissions, parsePermTag(perm)) {
			resource.Permissions = append(resource.Permissions, parsePermTag(perm))
		}
	}
}
