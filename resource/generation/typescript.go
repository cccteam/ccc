package generation

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
)

func (t *TypescriptGenerator) runTypescriptPermissionGeneration() error {
	templateData := t.rc.TypescriptData()

	if !t.genTypescriptMeta {
		if err := removeGeneratedFiles(t.typescriptDestination, HeaderComment); err != nil {
			return errors.Wrap(err, "removeGeneratedFiles()")
		}
	}

	output, err := t.generateTemplateOutput(typescriptPermissionTemplate, map[string]any{
		"Permissions":         templateData.Permissions,
		"Resources":           templateData.Resources,
		"ResourceTags":        templateData.ResourceTags,
		"ResourcePermissions": templateData.ResourcePermissions,
		"Domains":             templateData.Domains,
	})
	if err != nil {
		return errors.Wrap(err, "c.generateTemplateOutput()")
	}

	destinationFilePath := filepath.Join(t.typescriptDestination, "resourcePermissions.ts")
	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := t.writeBytesToFile(destinationFilePath, file, output, false); err != nil {
		return errors.Wrap(err, "c.writeBytesToFile()")
	}

	log.Printf("Generated Permissions: %s\n", file.Name())

	return nil
}

func (t *TypescriptGenerator) runTypescriptMetadataGeneration() error {
	if err := removeGeneratedFiles(t.typescriptDestination, HeaderComment); err != nil {
		return errors.Wrap(err, "removeGeneratedFiles()")
	}

	if err := t.generateTypescriptMetadata(); err != nil {
		return errors.Wrap(err, "generateTypescriptResources")
	}

	return nil
}

func (t *TypescriptGenerator) generateTemplateOutput(fileTemplate string, data map[string]any) ([]byte, error) {
	tmpl, err := template.New(fileTemplate).Funcs(t.templateFuncs()).Parse(fileTemplate)
	if err != nil {
		return nil, errors.Wrap(err, "template.Parse()")
	}

	buf := bytes.NewBuffer([]byte{})
	if err := tmpl.Execute(buf, data); err != nil {
		return nil, errors.Wrap(err, "tmpl.Execute()")
	}

	return buf.Bytes(), nil
}

func (t *TypescriptGenerator) templateFuncs() map[string]any {
	templateFuncs := map[string]any{
		"Pluralize": t.pluralize,
		"GoCamel":   strcase.ToGoCamel,
		"Camel":     t.caser.ToCamel,
		"Pascal":    t.caser.ToPascal,
		"Kebab":     t.caser.ToKebab,
		"Lower":     strings.ToLower,
		"PrimaryKeyTypeIsUUID": func(fields []*FieldInfo) bool {
			for _, f := range fields {
				if f.IsPrimaryKey {
					return f.GoType == "ccc.UUID"
				}
			}

			return false
		},
		"FormatPerm": func(s string) string {
			if s == "" {
				return ""
			}

			return ` perm:"` + s + `"`
		},
		"PrimaryKeyType": func(fields []*FieldInfo) string {
			for _, f := range fields {
				if f.IsPrimaryKey {
					return f.GoType
				}
			}

			return ""
		},
		"FormatQueryTag": func(query string) string {
			if query != "" {
				return " " + query
			}

			return ""
		},
		"FormatResourceInterfaceTypes": formatResourceInterfaceTypes,
		"FormatTokenTag":               t.formatTokenTags,
		"ResourceSearchType": func(searchType string) string {
			switch strings.ToUpper(searchType) {
			case "SUBSTRING":
				return "resource.SubString"
			case "FULLTEXT":
				return "resource.FullText"
			case "NGRAMS":
				return "resource.Ngram"
			default:
				return ""
			}
		},
		"DetermineParameters": func(structName string, route generatedRoute) string {
			if strings.EqualFold(route.Method, "get") && strings.HasSuffix(route.Path, fmt.Sprintf("{%sID}", strcase.ToGoCamel(structName))) {
				return fmt.Sprintf(`map[string]string{%q: %q}`, strcase.ToGoCamel(structName+"ID"), strcase.ToGoCamel(fmt.Sprintf("test%sID", t.caser.ToPascal(structName))))
			}

			return "map[string]string{}"
		},
		"MethodToHttpConst": func(method string) string {
			switch method {
			case "GET":
				return "http.MethodGet"
			case "POST":
				return "http.MethodPost"
			case "PATCH":
				return "http.MethodPatch"
			default:
				panic(fmt.Sprintf("MethodToHttpConst: unknown method: %s", method))
			}
		},
	}

	return templateFuncs
}

func (t *TypescriptGenerator) generateTypescriptMetadata() error {
	output, err := t.generateTemplateOutput(typescriptMetadataTemplate, map[string]any{
		"Resources":         t.resources,
		"ConsolidatedRoute": t.consolidatedRoute,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	destinationFilePath := filepath.Join(t.typescriptDestination, "resources.ts")
	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := t.writeBytesToFile(destinationFilePath, file, output, false); err != nil {
		return errors.Wrap(err, "c.writeBytesToFile()")
	}

	log.Printf("Generated Resource Metadata: %s\n", file.Name())

	return nil
}

func (t *TypescriptGenerator) generateTypescriptTemplate(fileTemplate string, data map[string]any) ([]byte, error) {
	tmpl, err := template.New(fileTemplate).Funcs(t.templateFuncs()).Parse(fileTemplate)
	if err != nil {
		return nil, errors.Wrap(err, "template.Parse()")
	}

	buf := bytes.NewBuffer([]byte{})
	if err := tmpl.Execute(buf, data); err != nil {
		return nil, errors.Wrap(err, "tmpl.Execute()")
	}

	return buf.Bytes(), nil
}
