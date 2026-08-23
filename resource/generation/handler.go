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
// (/{segment}/{domain}/{resource}/...); a global resource named like the domain route
// segment (the tenant-record pattern) shares the dispatcher's descent case, branching
// on path depth — its structural requirements are enforced by
// validateDomainSegmentResources.
func (r *resourceGenerator) consolidatedPatchResources() ([]*resourceInfo, error) {
	if err := r.validateDomainSegmentResources(); err != nil {
		return nil, err
	}

	var consolidated []*resourceInfo
	for _, res := range r.resources {
		if !res.IsConsolidated {
			continue
		}
		consolidated = append(consolidated, res)
	}

	return consolidated, nil
}

// validateDomainSegmentResources checks every resource whose route name equals the
// domain route segment — the tenant-record pattern, where /api/organizations lists the
// tenants and /api/organizations/{organizationID}/... serves tenant-scoped routes. The
// pattern is supported, with two structural requirements:
//
//   - the resource must have a single primary key, so its operation paths (depth ≤ 2)
//     can never be ambiguous with domain descents (depth ≥ 3) in the consolidated
//     handler, and its read route cannot shadow the segment pair's children; and
//   - its read-route parameter must be named exactly like the domain route parameter,
//     because chi permits one wildcard name per tree position — a mismatch panics at
//     route registration instead of failing generation.
//
// Only enforced when domain-scoped routes exist; without the segment pair there is
// nothing to interact with.
func (r *resourceGenerator) validateDomainSegmentResources() error {
	if !r.hasDomainScoped() {
		return nil
	}

	var errs []error
	check := func(name string, domainScoped, compoundPK bool, pkName string) {
		if strcase.ToKebab(r.pluralize(name)) != r.domainRouteSegment {
			return
		}
		if domainScoped {
			// Served under the segment pair itself; it shares no position with it.
			return
		}
		if compoundPK {
			errs = append(errs, errors.Newf("resource %s: its route name %q equals the domain route segment, so it must have a single primary key — multi-segment keys are ambiguous with domain-scoped paths", name, r.domainRouteSegment))

			return
		}
		if pkName == "" {
			// No primary key means no read route, so no shared wildcard position.
			return
		}
		if param := strcase.ToGoCamel(name + pkName); param != r.domainRouteParam {
			errs = append(errs, errors.Newf("resource %s: its route name %q equals the domain route segment, so its read-route parameter %q must equal the domain route parameter %q (chi permits one wildcard name per position); align them via WithDomainRoute", name, r.domainRouteSegment, param, r.domainRouteParam))
		}
	}

	for _, res := range r.resources {
		pkName := ""
		if pk := res.PrimaryKey(); pk != nil {
			pkName = pk.Name()
		}
		check(res.Name(), res.IsDomainScoped(), res.HasCompoundPrimaryKey(), pkName)
	}
	for _, res := range r.computedResources {
		pkName := ""
		if pk := res.PrimaryKey(); pk != nil {
			pkName = pk.Name()
		}
		check(res.Name(), res.IsDomainScoped(), res.HasCompoundPrimaryKey(), pkName)
	}

	if len(errs) != 0 {
		return errors.Wrap(errors.Join(errs...), "domain route segment resource error")
	}

	return nil
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
	var segmentCase *consolidatedCaseData
	for _, res := range resources {
		c := consolidatedCaseData{
			resourceInfo:    res,
			ResourcePackage: r.resource.Package(),
			ReceiverName:    r.receiverName,
		}
		switch {
		case res.IsDomainScoped():
			c.DomainPatternPrefix = domainPatternPrefix
			domainCases = append(domainCases, c)
		case strcase.ToKebab(r.pluralize(res.Name())) == r.domainRouteSegment:
			// The tenant-record pattern: this global resource shares the descent
			// case's name, so its case branches on path depth (validated single-PK,
			// keeping resource operations at depth ≤ 2 and descents at depth ≥ 3).
			segmentCase = &c
		default:
			globalCases = append(globalCases, c)
		}
	}
	if segmentCase != nil && len(domainCases) == 0 {
		// Without domain-scoped consolidated resources there is no descent case to
		// share; the segment-named resource dispatches like any other global case.
		globalCases = append(globalCases, *segmentCase)
		segmentCase = nil
	}

	if err := r.writeFormattedGoFile(destinationFilePath, "consolidatedPatchHandler", consolidatedPatchTemplate, &consolidatedPatchData{
		Source:              r.resource.Dir(),
		LocalPackageImports: r.localPackageImports(),
		Resources:           resources,
		GlobalCases:         globalCases,
		DomainCases:         domainCases,
		SegmentCase:         segmentCase,
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
