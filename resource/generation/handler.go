package generation

import (
	"bytes"
	"fmt"
	"log"
	"path/filepath"
	"slices"
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

	if err := r.generateDomainGuard(); err != nil {
		return errors.Wrap(err, "generateDomainGuard()")
	}

	if err := r.generateDecoders(); err != nil {
		return errors.Wrap(err, "generateDecoders()")
	}

	if err := r.generateAppContract(); err != nil {
		return errors.Wrap(err, "generateAppContract()")
	}

	if err := r.generatePermissions(); err != nil {
		return errors.Wrap(err, "generatePermissions()")
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

	// One consolidated dispatcher per outlet with members: each outlet's bundle
	// carries exactly the consolidated resources attached to it.
	for _, outlet := range r.allOutlets() {
		var members []*resourceInfo
		for _, res := range consolidatedResources {
			if res.OnOutlet(outlet.name) {
				members = append(members, res)
			}
		}
		if len(members) == 0 {
			continue
		}
		if err := r.generateConsolidatedPatchHandler(outlet, members); err != nil {
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
// pattern is supported, with one structural requirement: the resource must have a
// single primary key, so its operation paths (depth ≤ 2) can never be ambiguous with
// domain descents (depth ≥ 3) in the consolidated handler, and its read route cannot
// shadow the segment pair's children. (The read-route parameter needs no validation:
// deriveDomainRouteParam makes the domain route parameter equal it by construction.)
//
// Only enforced when domain-scoped routes exist; without the segment pair there is
// nothing to interact with.
func (r *resourceGenerator) validateDomainSegmentResources() error {
	if !r.hasDomainScoped() {
		return nil
	}

	var errs []error
	check := func(name string, domainScoped, compoundPK bool) {
		if strcase.ToKebab(r.pluralize(name)) != r.domainRouteSegment {
			return
		}
		if domainScoped {
			// Served under the segment pair itself; it shares no position with it.
			return
		}
		if compoundPK {
			errs = append(errs, errors.Newf("resource %s: its route name %q equals the domain route segment, so it must have a single primary key — multi-segment keys are ambiguous with domain-scoped paths", name, r.domainRouteSegment))
		}
	}

	for _, res := range r.resources {
		check(res.Name(), res.IsDomainScoped(), res.HasCompoundPrimaryKey())
	}
	for _, res := range r.computedResources {
		check(res.Name(), res.IsDomainScoped(), res.HasCompoundPrimaryKey())
	}

	if len(errs) != 0 {
		return errors.Wrap(errors.Join(errs...), "domain route segment resource error")
	}

	return nil
}

// generateDomainGuard emits the application's DomainGuard middleware whenever anything
// is domain-scoped — same gate as the router's Domain const. Emission does not depend
// on routing suppression: an application that registers a domain-scoped handler
// manually wraps it in DomainGuard itself.
func (r *resourceGenerator) generateDomainGuard() error {
	if !r.hasDomainScoped() {
		return nil
	}

	begin := time.Now()
	destinationFilePath := filepath.Join(r.handler.Dir(), generatedGoFileName(domainGuardOutputName))

	if err := r.writeFormattedGoFile(destinationFilePath, "domainGuardTemplate", domainGuardTemplate, &domainGuardData{
		Source:              r.resource.Dir(),
		Package:             r.handler.Package(),
		LocalPackageImports: r.localPackageImports(),
		ApplicationName:     r.applicationName,
		ReceiverName:        r.receiverName,
		ConcealedDomains:    r.concealedDomains,
	}); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}
	log.Printf("Generated domain guard file in %s: %s", time.Since(begin), destinationFilePath)

	return nil
}

// handlerFeatures reports which application methods the generated handlers draw on,
// scanning the same suppression-aware gates the handler generators emit under. It is
// the shared basis for the generated decoder constructors and the app contract.
type handlerFeatures struct {
	hasQuery    bool
	hasPatch    bool
	hasRPC      bool
	hasComputed bool
	// hasTargetedRPC reports a non-suppressed @target-bearing method: its
	// handler decodes through the targeted constructor, which carries a
	// conditional Execute decision to the frame instead of refusing it.
	hasTargetedRPC bool
	// rpcPackage qualifies the generated Method union; set iff hasRPC.
	rpcPackage string
}

func (r *resourceGenerator) handlerFeatures() handlerFeatures {
	var f handlerFeatures
	for _, res := range r.resources {
		for _, ht := range resourceEndpoints(res) {
			switch ht {
			case ListHandler, ReadHandler:
				f.hasQuery = true
			case PatchHandler:
				f.hasPatch = true
			default: // resourceEndpoints returns concrete handler types only.
			}
		}
		if hasConsolidatedHandler(res) {
			f.hasPatch = true
		}
	}
	if r.genComputedResources {
		for _, res := range r.computedResources {
			if !res.SuppressReadHandler || !res.SuppressListHandler {
				f.hasComputed = true
			}
		}
	}
	if r.genRPCMethods {
		for _, rpcMethod := range r.rpcMethods {
			if !rpcMethod.SuppressHandler {
				f.hasRPC = true
				f.rpcPackage = r.rpc.Package()
				if rpcMethod.Target != nil {
					f.hasTargetedRPC = true
				}
			}
		}
	}

	return f
}

// generateDecoders emits the decoder constructors generated handlers call: shims over
// the resource library's Must* constructors, constrained to the generated closed
// unions (Resourcer, Method). Each constructor is emitted only when a generated
// handler calls it.
func (r *resourceGenerator) generateDecoders() error {
	f := r.handlerFeatures()
	if !f.hasQuery && !f.hasComputed && !f.hasPatch && !f.hasRPC {
		return nil
	}

	begin := time.Now()
	destinationFilePath := filepath.Join(r.handler.Dir(), generatedGoFileName(decodersOutputName))

	if err := r.writeFormattedGoFile(destinationFilePath, "decodersTemplate", decodersTemplate, &decodersFileData{
		Source:                  r.resource.Dir(),
		Package:                 r.handler.Package(),
		LocalPackageImports:     r.localPackageImports(),
		ApplicationName:         r.applicationName,
		ReceiverName:            r.receiverName,
		RPCPackage:              f.rpcPackage,
		RouterPackage:           r.router.Package(),
		HasQueryDecoder:         f.hasQuery,
		HasComputedQueryDecoder: f.hasComputed,
		HasPatchDecoder:         f.hasPatch,
		HasRPCDecoder:           f.hasRPC,
		HasTargetedRPCDecoder:   f.hasTargetedRPC,
	}); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}
	log.Printf("Generated decoders file in %s: %s", time.Since(begin), destinationFilePath)

	return nil
}

// generateAppContract emits the compile-time assertions for the methods the generated
// code draws from the application type: interfaces where the generator knows the
// signature, method expressions for RPCClient and ComputedClient, whose return types
// are application-owned. Each feature's block is emitted only while that feature
// generates a caller, so dropping a feature leaves the methods it drew on visibly
// unasserted on the next regeneration.
func (r *resourceGenerator) generateAppContract() error {
	f := r.handlerFeatures()

	begin := time.Now()
	destinationFilePath := filepath.Join(r.handler.Dir(), generatedGoFileName(appContractOutputName))

	if err := r.writeFormattedGoFile(destinationFilePath, "appContractTemplate", appContractTemplate, &appContractData{
		Source:              r.resource.Dir(),
		Package:             r.handler.Package(),
		LocalPackageImports: r.localPackageImports(),
		ApplicationName:     r.applicationName,
		HasValidator:        f.hasPatch || f.hasRPC,
		HasDomainScoped:     r.hasDomainScoped(),
		HasRPC:              f.hasRPC,
		HasComputed:         f.hasComputed,
		ConcealedDomains:    r.concealedDomains,
	}); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}
	log.Printf("Generated app contract file in %s: %s", time.Since(begin), destinationFilePath)

	return nil
}

// generatePermissions emits the application's PermissionDigest and UserDomains
// handlers — delegations to the library-owned handlers — unconditionally: every
// generated application serves both permission endpoints on its default outlet,
// wiring nothing.
func (r *resourceGenerator) generatePermissions() error {
	begin := time.Now()
	destinationFilePath := filepath.Join(r.handler.Dir(), generatedGoFileName(permissionsOutputName))

	if err := r.writeFormattedGoFile(destinationFilePath, "permissionsTemplate", permissionsTemplate, &permissionsData{
		Source:                 r.resource.Dir(),
		Package:                r.handler.Package(),
		ApplicationName:        r.applicationName,
		ReceiverName:           r.receiverName,
		RoutePrefix:            r.routePrefix,
		HasExtraSessionOutlets: slices.ContainsFunc(r.extraOutlets, func(outlet routerOutlet) bool { return outlet.servesSessions }),
	}); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}
	log.Printf("Generated permissions file in %s: %s", time.Since(begin), destinationFilePath)

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

func (r *resourceGenerator) generateConsolidatedPatchHandler(outlet routerOutlet, resources []*resourceInfo) error {
	begin := time.Now()
	outputName := consolidatedHandlerOutputName
	if outlet.name != defaultOutletName {
		outputName += "_" + strcase.ToSnake(outlet.name)
	}
	fileName := generatedGoFileName(outputName)
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
		HandlerName:         fmt.Sprintf("Patch%sResources", outlet.suffix()),
		ConcealedDomains:    r.concealedDomains,
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
