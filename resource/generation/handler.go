package generation

import (
	"bytes"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
)

func (r *resourceGenerator) runHandlerGeneration() error {
	if err := removeGeneratedFiles(r.handler.Dir(), prefix); err != nil {
		return errors.Wrap(err, "removeGeneratedFiles()")
	}

	if err := r.generateResourceInterfaces(); err != nil {
		return errors.Wrap(err, "c.generateResourceInterfaces()")
	}

	if err := forEachGo(r.resources, r.generateHandlers); err != nil {
		return err
	}

	if r.genRPCMethods {
		rpcMethods := make([]*rpcMethodInfo, 0, len(r.rpcMethods))
		for _, rpcMethod := range r.rpcMethods {
			if !rpcMethod.SuppressHandler {
				rpcMethods = append(rpcMethods, rpcMethod)
			}
		}

		if err := forEachGo(rpcMethods, r.generateRPCHandler); err != nil {
			return err
		}
	}

	if r.genComputedResources {
		if err := forEachGo(r.computedResources, r.generateComputedResourceHandler); err != nil {
			return err
		}
	}

	var consolidatedResources []*resourceInfo
	for _, res := range r.resources {
		if res.IsConsolidated {
			consolidatedResources = append(consolidatedResources, res)
		}
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
		destinationFilePath := filepath.Join(r.handler.Dir(), fileName)

		if err := r.writeFormattedGoFile(destinationFilePath, "handlers", handlerHeaderTemplate, &handlersFileData{
			Source:              r.resource.Dir(),
			LocalPackageImports: r.localPackageImports(),
			Handlers:            string(bytes.Join(handlerData, []byte("\n\n"))),
			Package:             r.handler.Package(),
			resource:            res,
		}); err != nil {
			return errors.Wrap(err, "writeFormattedGoFile()")
		}
		log.Printf("Generated handler file in %s: %s", time.Since(begin), destinationFilePath)
	}

	return nil
}

func (r *resourceGenerator) generateConsolidatedPatchHandler(resources []*resourceInfo) error {
	begin := time.Now()
	fileName := generatedGoFileName(consolidatedHandlerOutputName)
	destinationFilePath := filepath.Join(r.handler.Dir(), fileName)

	if err := r.writeFormattedGoFile(destinationFilePath, "consolidatedPatchHandler", consolidatedPatchTemplate, &consolidatedPatchData{
		Source:              r.resource.Dir(),
		LocalPackageImports: r.localPackageImports(),
		Resources:           resources,
		Package:             r.handler.Package(),
		ResourcePackage:     r.resource.Package(),
		ApplicationName:     r.applicationName,
		ReceiverName:        r.receiverName,
	}); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}

	log.Printf("Generated consolidated handler file in %s: %s", time.Since(begin), destinationFilePath)

	return nil
}

func (r *resourceGenerator) handlerContent(handler HandlerType, res *resourceInfo) ([]byte, error) {
	output, err := r.generateTemplateOutput("handler", handler.template(), handlerContentData{
		ResourcePackage:         r.resource.Package(),
		Resource:                res,
		VirtualResourcesPackage: r.virtual.Package(),
		ApplicationName:         r.applicationName,
		ReceiverName:            r.receiverName,
	})
	if err != nil {
		return nil, errors.Wrap(err, "generateTemplateOutput()")
	}

	return output, nil
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
