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

func (r *resourceGenerator) runResourcesGeneration() error {
	if err := removeGeneratedFiles(r.resourceDestination, Prefix); err != nil {
		return err
	}

	if err := r.generateResourceInterfaces(); err != nil {
		return errors.Wrap(err, "c.generateResourceInterfaces()")
	}

	for _, resource := range r.resources {
		if err := r.generateResources(resource); err != nil {
			return errors.Wrap(err, "c.generateResources()")
		}
	}

	if err := r.generateResourceTests(); err != nil {
		return errors.Wrap(err, "c.generateResourceTests()")
	}

	return nil
}

func (r *resourceGenerator) generateResourceInterfaces() error {
	output, err := r.generateTemplateOutput("resourcesInterfaceTemplate", resourcesInterfaceTemplate, map[string]any{
		"Source": r.resourceFilePath,
		"Types":  r.resources,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	destinationFile := filepath.Join(r.resourceDestination, generatedFileName(resourceInterfaceOutputName))

	file, err := os.Create(destinationFile)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := r.writeBytesToFile(destinationFile, file, output, true); err != nil {
		return errors.Wrap(err, "c.writeBytesToFile()")
	}

	return nil
}

func (r *resourceGenerator) generateResourceTests() error {
	output, err := r.generateTemplateOutput("resourcesTestTemplate", resourcesTestTemplate, map[string]any{
		"Source":    r.resourceFilePath,
		"Resources": r.resources,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	destinationFile := filepath.Join(r.resourceDestination, resourcesTestFileName)

	file, err := os.Create(destinationFile)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := r.writeBytesToFile(destinationFile, file, output, true); err != nil {
		return errors.Wrap(err, "c.writeBytesToFile()")
	}

	return nil
}

func (r *resourceGenerator) generateResources(res *resourceInfo) error {
	fileName := generatedFileName(strings.ToLower(r.caser.ToSnake(r.pluralize(res.Name))))
	destinationFilePath := filepath.Join(r.resourceDestination, fileName)

	log.Printf("Generating resource file: %v\n", fileName)

	output, err := r.generateTemplateOutput("resourceFileTemplate", resourceFileTemplate, map[string]any{
		"Source":   r.resourceFilePath,
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

	if err := r.writeBytesToFile(destinationFilePath, file, output, true); err != nil {
		return errors.Wrap(err, "c.writeBytesToFile()")
	}

	return nil
}

func (r *resourceGenerator) generateTemplateOutput(templateName, fileTemplate string, data map[string]any) ([]byte, error) {
	tmpl, err := template.New(templateName).Funcs(r.templateFuncs()).Parse(fileTemplate)
	if err != nil {
		return nil, errors.Wrap(err, "template.Parse()")
	}

	buf := bytes.NewBuffer([]byte{})
	if err := tmpl.Execute(buf, data); err != nil {
		return nil, errors.Wrap(err, "tmpl.Execute()")
	}

	return buf.Bytes(), nil
}

func (c *client) buildTableSearchIndexes(tableName string) []*searchIndex {
	typeIndexMap := make(map[resource.FilterType]string)
	if tableMeta, ok := c.tableMap[tableName]; ok {
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
