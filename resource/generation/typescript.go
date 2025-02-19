package generation

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"text/template"

	"github.com/go-playground/errors/v5"
)

func (c *Client) runTypescriptPermissionGeneration() error {
	templateData := c.rc.TypescriptData()

	if !c.genTypescriptMeta {
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
	output, err := c.generateTemplateOutput(typescriptMetadataTemplate, map[string]any{
		"Resources": c.resources,
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
