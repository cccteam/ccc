package generation

import (
	"bytes"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
)

func (r *resourceGenerator) runHandlerGeneration() error {
	if err := removeGeneratedFiles(r.handler.Dir(), prefix); err != nil {
		return errors.Wrap(err, "removeGeneratedFiles()")
	}

	if err := r.generateResourceInterfaces(); err != nil {
		return errors.Wrap(err, "generateResourceInterfaces()")
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

	consolidatedResources, err := r.consolidatedPatchResources()
	if err != nil {
		return err
	}

	if len(consolidatedResources) > 0 {
		if err := r.generateConsolidatedPatchHandler(consolidatedResources); err != nil {
			return errors.Wrap(err, "generateConsolidatedPatchHandler()")
		}
	}

	return nil
}

// consolidatedPatchResources returns the resources served by the consolidated patch
// handler. Domain-scoped resources participate with domain-embedded operation paths
// (/{segment}/{domain}/{resource}/...), so no consolidated resource may be named like
// the domain route segment — that name is the batch dispatcher's descent case.
func (r *resourceGenerator) consolidatedPatchResources() ([]*resourceInfo, error) {
	var consolidated []*resourceInfo
	for _, res := range r.resources {
		if !res.IsConsolidated {
			continue
		}
		if kebab := strcase.ToKebab(r.pluralize(res.Name())); kebab == r.domainRouteSegment {
			return nil, errors.Newf("resource %s: its route name %q collides with the domain route segment; rename the resource or choose another segment via WithDomainRoute", res.Name(), kebab)
		}
		consolidated = append(consolidated, res)
	}

	return consolidated, nil
}

func (r *resourceGenerator) generateHandlers(res *resourceInfo) error {
	handlerTypes := resourceEndpoints(res)

	handlerData := make([][]byte, 0, len(handlerTypes))
	for _, handlerTyp := range handlerTypes {
		data, err := r.handlerContent(handlerTyp, res)
		if err != nil {
			return errors.Wrap(err, "handlerContent()")
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

	domainPatternPrefix := fmt.Sprintf("/%s/{%s}", r.domainRouteSegment, r.domainRouteParam)
	var globalCases, domainCases []consolidatedCaseData
	for _, res := range resources {
		c := consolidatedCaseData{
			resourceInfo:    res,
			ResourcePackage: r.resource.Package(),
			ReceiverName:    r.receiverName,
		}
		if res.IsDomainScoped() {
			c.DomainPatternPrefix = domainPatternPrefix
			domainCases = append(domainCases, c)
		} else {
			globalCases = append(globalCases, c)
		}
	}

	if err := r.writeFormattedGoFile(destinationFilePath, "consolidatedPatchHandler", consolidatedPatchTemplate, &consolidatedPatchData{
		Source:              r.resource.Dir(),
		LocalPackageImports: r.localPackageImports(),
		Resources:           resources,
		GlobalCases:         globalCases,
		DomainCases:         domainCases,
		DomainRouteSegment:  r.domainRouteSegment,
		DomainPatternPrefix: domainPatternPrefix,
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
