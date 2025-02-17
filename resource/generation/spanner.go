package generation

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/cccteam/ccc/resource"
	"github.com/go-playground/errors/v5"
)

func (c *Client) runResourcesGeneration() error {
	if err := c.generateResourceInterfaces(); err != nil {
		return errors.Wrap(err, "c.generateResourceInterfaces()")
	}

	for _, resource := range c.resources {
		if err := c.generatePatcherTypes(resource); err != nil {
			return errors.Wrap(err, "c.generatePatcherTypes()")
		}
	}

	if err := c.generateResourceTests(); err != nil {
		return errors.Wrap(err, "c.generateResourceTests()")
	}

	return nil
}

func (c *Client) generateResourceInterfaces() error {
	output, err := c.generateTemplateOutput(resourcesInterfaceTemplate, map[string]any{
		"Source": c.resourceFilePath,
		"Types":  c.resources,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	destinationFile := filepath.Join(c.resourceDestination, generatedFileName(resourceInterfaceOutputName))

	file, err := os.Create(destinationFile)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := c.writeBytesToFile(destinationFile, file, output, true); err != nil {
		return errors.Wrap(err, "c.writeBytesToFile()")
	}

	return nil
}

func (c *Client) generateResourceTests() error {
	output, err := c.generateTemplateOutput(resourcesTestTemplate, map[string]any{
		"Source":    c.resourceFilePath,
		"Resources": c.resources,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	destinationFile := filepath.Join(c.resourceDestination, resourcesTestFileName)

	file, err := os.Create(destinationFile)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := c.writeBytesToFile(destinationFile, file, output, true); err != nil {
		return errors.Wrap(err, "c.writeBytesToFile()")
	}

	return nil
}

func (c *Client) generatePatcherTypes(res *ResourceInfo) error {
	fileName := generatedFileName(strings.ToLower(c.caser.ToSnake(c.pluralize(res.Name))))
	destinationFilePath := filepath.Join(c.resourceDestination, fileName)

	log.Printf("Generating resource file: %v\n", fileName)

	output, err := c.generateTemplateOutput(resourceFileTemplate, map[string]any{
		"Source":   c.resourceFilePath,
		"Resource": res,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := c.writeBytesToFile(destinationFilePath, file, output, true); err != nil {
		return errors.Wrap(err, "c.writeBytesToFile()")
	}

	return nil
}

func (c *Client) generateTemplateOutput(fileTemplate string, data map[string]any) ([]byte, error) {
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

func (c *Client) buildTableSearchIndexes(tableName string) []*searchIndex {
	typeIndexMap := make(map[resource.SearchType]string)
	if tableMeta, ok := c.tableLookup[tableName]; ok {
		for tokenListColumn, expressionFields := range tableMeta.SearchIndexes {
			for _, exprField := range expressionFields {
				typeIndexMap[exprField.tokenType] = tokenListColumn
			}
		}
	}

	var indexes []*searchIndex
	for tokenType, indexName := range typeIndexMap {
		indexes = append(indexes, &searchIndex{
			Name:       indexName,
			SearchType: string(tokenType),
		})
	}

	return indexes
}
