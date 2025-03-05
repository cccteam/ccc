package generation

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"slices"
	"text/template"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

func (t *typescriptGenerator) runTypescriptPermissionGeneration() error {
	if !t.genMetadata {
		if err := removeGeneratedFiles(t.typescriptDestination, HeaderComment); err != nil {
			return errors.Wrap(err, "removeGeneratedFiles()")
		}
	}

	log.Println("Starting typescript resource permission generation...")

	routerData := t.rc.TypescriptData()

	templateData := map[string]any{
		"Permissions":            routerData.Permissions,
		"ResourcePermissions":    routerData.ResourcePermissions,
		"Resources":              routerData.Resources,
		"ResourceTags":           routerData.ResourceTags,
		"ResourcePermissionsMap": routerData.ResourcePermissionMap,
		"Domains":                routerData.Domains,
	}

	if t.genRPCMethods {
		templateData["RPCMethods"] = t.rpcMethods
	}

	output, err := t.generateTemplateOutput(typescriptPermissionTemplate, templateData)
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

func (t *typescriptGenerator) runTypescriptMetadataGeneration() error {
	if err := removeGeneratedFiles(t.typescriptDestination, HeaderComment); err != nil {
		return errors.Wrap(err, "removeGeneratedFiles()")
	}

	if err := t.generateTypescriptMetadata(); err != nil {
		return errors.Wrap(err, "generateTypescriptResources")
	}

	return nil
}

func (t *typescriptGenerator) generateTemplateOutput(fileTemplate string, data map[string]any) ([]byte, error) {
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

func (t *typescriptGenerator) generateTypescriptMetadata() error {
	log.Println("Starting typescript metadata generation...")
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

func (t *typescriptGenerator) setTypescriptInfo(resource *resourceInfo) (*resourceInfo, error) {
	for _, field := range resource.Fields {
		var err error
		field.typescriptType, err = decodeToTypescriptType(field.tt, t.typescriptOverrides)
		if err != nil {
			return nil, errors.Wrapf(err, "could not decode typescript type for field %q in struct %q at %s:%v", field.Name(), resource.Name(), field.PackageName(), field.Position())
		}

		if field.IsForeignKey && slices.Contains(t.routerResources, accesstypes.Resource(field.ReferencedResource)) {
			field.IsEnumerated = true
		}
	}

	return resource, nil
}
