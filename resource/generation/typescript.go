package generation

import (
	"log"
	"os"
	"path/filepath"

	"github.com/go-playground/errors/v5"
)

func (c *GenerationClient) RunTypescriptGeneration() error {
	if err := removeGeneratedFiles(c.typescriptDestination, HeaderComment); err != nil {
		return errors.Wrap(err, "removeGeneratedFiles()")
	}

	if err := c.generateTypescriptResources(); err != nil {
		return errors.Wrap(err, "generateTypescriptResources")
	}

	return nil
}

func (c *GenerationClient) generateTypescriptResources() error {
	log.Println("Generating resources.ts file")

	s := c.resourceCollection

	output, err := c.generateTemplateOutput(typescriptTemplate, map[string]any{
		"Permissions":         s.TSPermissions(),
		"Resources":           s.TSResources(),
		"ResourceTags":        s.TSTags(),
		"ResourcePermissions": s.TSResourcePermissions(),
		"Domains":             s.TSDomains(),
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

	return nil
}
