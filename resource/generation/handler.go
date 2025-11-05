package generation

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/go-playground/errors/v5"
)

func (r *resourceGenerator) runHandlerGeneration() error {
	if err := removeGeneratedFiles(r.handlerDestination, prefix); err != nil {
		return errors.Wrap(err, "removeGeneratedFiles()")
	}

	if err := r.generateResourceInterfaces(); err != nil {
		return errors.Wrap(err, "c.generateResourceInterfaces()")
	}

	var (
		consolidatedResources []*resourceInfo
		wg                    sync.WaitGroup

		errChan = make(chan error)
	)
	for _, res := range r.resources {
		wg.Add(1)
		go func() {
			if err := r.generateHandlers(res); err != nil {
				errChan <- err
			}
			wg.Done()
		}()

		if res.IsConsolidated {
			consolidatedResources = append(consolidatedResources, res)
		}
	}

	if r.genRPCMethods {
		for _, rpcMethod := range r.rpcMethods {
			wg.Go(func() {
				if err := r.generateRPCHandler(rpcMethod); err != nil {
					errChan <- err
				}
			})
		}
	}

	if r.genComputedResources {
		for _, res := range r.computedResources {
			wg.Go(func() {
				if err := r.generateComputedResourceHandler(res); err != nil {
					errChan <- err
				}
			})
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
		if err := r.generateConsolidatedPatchHandler(consolidatedResources); err != nil {
			return errors.Wrap(err, "generateConsolidatedPatchHandler()")
		}
	}

	return nil
}

func (r *resourceGenerator) generateHandlers(res *resourceInfo) error {
	handlerTypes := resourceEndpoints(res)

	handlerData := make([][]byte, 0, len(handlerTypes))
	for _, handlerTyp := range handlerTypes {
		data, err := r.handlerContent(handlerTyp, res)
		if err != nil {
			return errors.Wrap(err, "replaceHandlerFileContent()")
		}

		handlerData = append(handlerData, data)
	}

	if len(handlerData) > 0 {
		begin := time.Now()
		fileName := generatedGoFileName(strings.ToLower(caser.ToSnake(r.pluralize(res.Name()))))
		destinationFilePath := filepath.Join(r.handlerDestination, fileName)

		file, err := os.Create(destinationFilePath)
		if err != nil {
			return errors.Wrap(err, "os.Create()")
		}
		defer file.Close()

		tmpl, err := template.New("handlers").Funcs(r.templateFuncs()).Parse(handlerHeaderTemplate)
		if err != nil {
			return errors.Wrap(err, "template.New().Parse()")
		}

		buf := bytes.NewBuffer(nil)
		if err := tmpl.Execute(buf, map[string]any{
			"Source":              r.resourcePackageDir,
			"LocalPackageImports": r.localPackageImports(),
			"Handlers":            string(bytes.Join(handlerData, []byte("\n\n"))),
			"Package":             filepath.Base(r.handlerDestination),
		}); err != nil {
			return errors.Wrap(err, "tmpl.Execute()")
		}

		formattedBytes, err := r.GoFormatBytes(file.Name(), buf.Bytes())
		if err != nil {
			return err
		}

		if err := r.WriteBytesToFile(file, formattedBytes); err != nil {
			return err
		}
		log.Printf("Generated handler file in %s: %s", time.Since(begin), destinationFilePath)
	}

	return nil
}

func (r *resourceGenerator) generateConsolidatedPatchHandler(resources []*resourceInfo) error {
	begin := time.Now()
	fileName := generatedGoFileName(consolidatedHandlerOutputName)
	destinationFilePath := filepath.Join(r.handlerDestination, fileName)

	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	tmpl, err := template.New("consolidatedHandler").Funcs(r.templateFuncs()).Parse(consolidatedPatchTemplate)
	if err != nil {
		return errors.Wrap(err, "template.New().Parse()")
	}

	buf := bytes.NewBuffer(nil)
	if err := tmpl.Execute(buf, map[string]any{
		"Source":              r.resourcePackageDir,
		"LocalPackageImports": r.localPackageImports(),
		"Resources":           resources,
		"Package":             filepath.Base(r.handlerDestination),
		"ApplicationName":     r.applicationName,
		"ReceiverName":        r.receiverName,
	}); err != nil {
		return errors.Wrap(err, "tmpl.Execute()")
	}

	formattedBytes, err := r.GoFormatBytes(file.Name(), buf.Bytes())
	if err != nil {
		return err
	}

	if err := r.WriteBytesToFile(file, formattedBytes); err != nil {
		return err
	}

	log.Printf("Generated consolidated handler file in %s: %s", time.Since(begin), destinationFilePath)

	return nil
}

func (r *resourceGenerator) handlerContent(handler HandlerType, res *resourceInfo) ([]byte, error) {
	tmpl, err := template.New("handler").Funcs(r.templateFuncs()).Parse(handler.template())
	if err != nil {
		return nil, errors.Wrap(err, "template.New().Parse()")
	}

	buf := bytes.NewBuffer([]byte{})
	if err := tmpl.Execute(buf, map[string]any{
		"Resource":        res,
		"ApplicationName": r.applicationName,
		"ReceiverName":    r.receiverName,
	}); err != nil {
		return nil, errors.Wrap(err, "tmpl.Execute()")
	}

	return buf.Bytes(), nil
}

func (c *client) handlerName(structName string, handlerType HandlerType) string {
	var functionName string
	switch handlerType {
	case ListHandler:
		functionName = c.pluralize(structName)
	case ReadHandler:
		functionName = structName
	case PatchHandler:
		functionName = "Patch" + c.pluralize(structName)
	default:
		panic(fmt.Sprintf("unexpected HandlerType: %q", handlerType))
	}

	return functionName
}
