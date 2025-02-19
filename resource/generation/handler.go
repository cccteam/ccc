package generation

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"text/template"

	"github.com/go-playground/errors/v5"
)

func (c *Client) runHandlerGeneration() error {
	if err := removeGeneratedFiles(c.handlerDestination, HeaderComment); err != nil {
		return errors.Wrap(err, "removeGeneratedFiles()")
	}

	var (
		consolidatedResources []*ResourceInfo
		wg                    sync.WaitGroup

		errChan = make(chan error)
	)
	for _, resource := range c.resources {
		wg.Add(1)
		go func(resource *ResourceInfo) {
			if err := c.generateHandlers(resource); err != nil {
				errChan <- err
			}
			wg.Done()
		}(resource)

		if !resource.IsView && slices.Contains(c.consolidatedResourceNames, resource.Name) != c.consolidateAll {
			consolidatedResources = append(consolidatedResources, resource)
		}
	}

	go func() {
		wg.Wait()
		close(errChan)
	}()

	var handlerErrors error
	for e := range errChan {
		handlerErrors = errors.Join(handlerErrors, e)
	}

	if handlerErrors != nil {
		return errors.Wrap(handlerErrors, "runHandlerGeneration()")
	}

	if len(consolidatedResources) > 0 {
		if err := c.generateConsolidatedPatchHandler(consolidatedResources); err != nil {
			return errors.Wrap(err, "generateConsolidatedPatchHandler()")
		}
	}

	return nil
}

func (c *Client) generateHandlers(resource *ResourceInfo) error {
	generatedHandlers := []*generatedHandler{
		{
			template:    listTemplate,
			handlerType: List,
		},
	}

	if !resource.IsView {
		generatedHandlers = append(generatedHandlers, &generatedHandler{
			template:    readTemplate,
			handlerType: Read,
		})

		if slices.Contains(c.consolidatedResourceNames, resource.Name) == c.consolidateAll {
			generatedHandlers = append(generatedHandlers, &generatedHandler{
				template:    patchTemplate,
				handlerType: Patch,
			})
		}
	}

	opts := make(map[HandlerType]map[OptionType]any)
	for handlerType, options := range c.handlerOptions[resource.Name] {
		opts[handlerType] = make(map[OptionType]any)
		for _, option := range options {
			opts[handlerType][option] = struct{}{}
		}
	}

	var handlerData [][]byte
	for _, h := range generatedHandlers {
		if _, skipGeneration := opts[h.handlerType][NoGenerate]; !skipGeneration {
			data, err := c.handlerContent(h, resource)
			if err != nil {
				return errors.Wrap(err, "replaceHandlerFileContent()")
			}

			handlerData = append(handlerData, data)
		}
	}

	if len(handlerData) > 0 {
		fileName := generatedFileName(strings.ToLower(c.caser.ToSnake(c.pluralize(resource.Name))))
		destinationFilePath := filepath.Join(c.handlerDestination, fileName)

		file, err := os.Create(destinationFilePath)
		if err != nil {
			return errors.Wrap(err, "os.Create()")
		}
		defer file.Close()

		tmpl, err := template.New("handlers").Funcs(c.templateFuncs()).Parse(handlerHeaderTemplate)
		if err != nil {
			return errors.Wrap(err, "template.New().Parse()")
		}

		buf := bytes.NewBuffer(nil)
		if err := tmpl.Execute(buf, map[string]any{
			"Source":      c.resourceFilePath,
			"PackageName": c.packageName,
			"Handlers":    string(bytes.Join(handlerData, []byte("\n\n"))),
		}); err != nil {
			return errors.Wrap(err, "tmpl.Execute()")
		}

		log.Printf("Generating handler file: %s", fileName)

		if err := c.writeBytesToFile(destinationFilePath, file, buf.Bytes(), true); err != nil {
			return err
		}
	}

	return nil
}

func (c *Client) generateConsolidatedPatchHandler(resources []*ResourceInfo) error {
	fileName := generatedFileName(consolidatedHandlerOutputName)
	destinationFilePath := filepath.Join(c.handlerDestination, fileName)

	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	tmpl, err := template.New("consolidatedHandler").Funcs(c.templateFuncs()).Parse(consolidatedPatchTemplate)
	if err != nil {
		return errors.Wrap(err, "template.New().Parse()")
	}

	buf := bytes.NewBuffer(nil)
	if err := tmpl.Execute(buf, map[string]any{
		"Source":      c.resourceFilePath,
		"PackageName": c.handlerPackageName,
		"Resources":   resources,
	}); err != nil {
		return errors.Wrap(err, "tmpl.Execute()")
	}

	log.Printf("Generating consolidated handler file: %s", fileName)

	if err := c.writeBytesToFile(destinationFilePath, file, buf.Bytes(), true); err != nil {
		return err
	}

	return nil
}

func (c *Client) handlerContent(handler *generatedHandler, resource *ResourceInfo) ([]byte, error) {
	tmpl, err := template.New("handler").Funcs(c.templateFuncs()).Parse(handler.template)
	if err != nil {
		return nil, errors.Wrap(err, "template.New().Parse()")
	}

	buf := bytes.NewBuffer([]byte{})
	if err := tmpl.Execute(buf, map[string]any{
		"Resource": resource,
	}); err != nil {
		return nil, errors.Wrap(err, "tmpl.Execute()")
	}

	return buf.Bytes(), nil
}

func (c *Client) handlerName(structName string, handlerType HandlerType) string {
	var functionName string
	switch handlerType {
	case List:
		functionName = c.pluralize(structName)
	case Read:
		functionName = structName
	case Patch:
		functionName = "Patch" + c.pluralize(structName)
	}

	return functionName
}

func joinBytes(p ...[]byte) []byte {
	return bytes.Join(p, []byte(""))
}
