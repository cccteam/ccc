package generation

import (
	"bytes"
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"time"

	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
)

func (r *resourceGenerator) runHandlerGeneration() error {
	r.output.registerOutput(r.handler.Dir(), prefix)

	if err := r.generateResourceInterfaces(); err != nil {
		return errors.Wrap(err, "generateResourceInterfaces()")
	}

	if err := r.generateDomainGuard(); err != nil {
		return errors.Wrap(err, "generateDomainGuard()")
	}

	if err := r.generateTenants(); err != nil {
		return errors.Wrap(err, "generateTenants()")
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

	if err := r.generateLive(); err != nil {
		return errors.Wrap(err, "generateLive()")
	}

	if err := r.generateFeatures(); err != nil {
		return errors.Wrap(err, "generateFeatures()")
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

	consolidatedResources := r.consolidatedPatchResources()

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
		if err := r.generateConsolidatedPatchHandler(&outlet, members); err != nil {
			return errors.Wrap(err, "generateConsolidatedPatchHandler()")
		}
	}

	return nil
}

// consolidatedPatchResources returns the resources served by the consolidated patch
// handler. Domain-scoped resources participate with domain-embedded operation paths
// (/{segment}/{domain}/{resource}/...); the tenant record (@tenant), whose route name is
// the domain route segment, shares the dispatcher's descent case, branching on path
// depth: its single key (checked at capture) keeps its operations at depth 2 or less
// and the descents at depth 3 or more.
func (r *resourceGenerator) consolidatedPatchResources() []*resourceInfo {
	var consolidated []*resourceInfo
	for _, res := range r.resources {
		if !res.IsConsolidated {
			continue
		}
		consolidated = append(consolidated, res)
	}

	return consolidated
}

// generateDomainGuard emits the application's DomainGuard middleware whenever anything
// is domain-scoped — same gate as the router's Domain const, and the tenant record is
// then declared (resolveTenantRecord), so the roster the guard asks exists. Emission
// does not depend on routing suppression: an application that registers a
// domain-scoped handler manually wraps it in DomainGuard itself.
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
	// hasFileDecoder and hasComputedFileDecoder report routed @file declarations on
	// table or view resources and on computed resources whose read route is served:
	// each emits its decoder constructor.
	hasFileDecoder         bool
	hasComputedFileDecoder bool
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
		if !res.RoutingDisabled() && !res.ReadHandlerDisabled() && len(res.Files) > 0 {
			f.hasFileDecoder = true
		}
	}
	if r.genComputedResources {
		for _, res := range r.computedResources {
			if !res.ReadHandlerDisabled() || !res.SuppressListHandler {
				f.hasComputed = true
			}
			if !res.RoutingDisabled() && !res.ReadHandlerDisabled() && len(res.Files) > 0 {
				f.hasComputedFileDecoder = true
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
	if !f.hasQuery && !f.hasComputed && !f.hasPatch && !f.hasRPC && !f.hasFileDecoder && !f.hasComputedFileDecoder {
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
		HasCollection:           r.genRoutes,
		HasTargetedRPCDecoder:   f.hasTargetedRPC,
		HasFileDecoder:          f.hasFileDecoder,
		HasComputedFileDecoder:  f.hasComputedFileDecoder,
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
		LocalPackageImports:    r.localPackageImports(),
		ResourcePackage:        r.resource.Package(),
		RouterPackage:          r.router.Package(),
	}); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}
	log.Printf("Generated permissions file in %s: %s", time.Since(begin), destinationFilePath)

	return nil
}

// generateLive emits the application's live route handlers — LiveRenew,
// LiveUnsubscribe and LiveToken — as delegations to the library-owned handlers over
// the application's LiveService, unconditionally: every generated application serves
// the live routes on each session-serving outlet, wiring only the service.
func (r *resourceGenerator) generateLive() error {
	begin := time.Now()
	destinationFilePath := filepath.Join(r.handler.Dir(), generatedGoFileName(liveOutputName))

	extraSessionOutlets := slices.ContainsFunc(r.extraOutlets, func(outlet routerOutlet) bool {
		return outlet.servesSessions
	})
	if err := r.writeFormattedGoFile(destinationFilePath, "liveTemplate", liveTemplate, &permissionsData{
		Source:                 r.resource.Dir(),
		Package:                r.handler.Package(),
		ApplicationName:        r.applicationName,
		ReceiverName:           r.receiverName,
		RoutePrefix:            r.routePrefix,
		HasExtraSessionOutlets: extraSessionOutlets,
	}); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}
	log.Printf("Generated live file in %s: %s", time.Since(begin), destinationFilePath)

	return nil
}

func (r *resourceGenerator) generateHandlers(res *resourceInfo) error {
	handlerTypes := resourceEndpoints(res)

	handlerData := make([][]byte, 0, len(handlerTypes)+len(res.Files))
	for _, handlerTyp := range handlerTypes {
		data, err := r.handlerContent(handlerTyp, res)
		if err != nil {
			return errors.Wrap(err, "handlerContent()")
		}

		handlerData = append(handlerData, data)
	}
	// The @file routes hang under the read route, so their handlers live in the same
	// file as the resource's; a resource whose routing is off, or whose read is
	// suppressed, generates none: its @file columns then only name the keys the
	// release and the orphaned-file cleanup read.
	if !res.RoutingDisabled() && !res.ReadHandlerDisabled() {
		for _, file := range res.Files {
			data, err := r.fileHandlerContent(res, file)
			if err != nil {
				return errors.Wrap(err, "fileHandlerContent()")
			}

			handlerData = append(handlerData, data)
		}
	}

	if len(handlerData) > 0 {
		begin := time.Now()
		fileName := generatedGoFileName(fileStem(r.pluralize(res.Name())))
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

func (r *resourceGenerator) generateConsolidatedPatchHandler(outlet *routerOutlet, resources []*resourceInfo) error {
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
		case res.IsTenant:
			// The tenant record shares the descent case's name, so its case branches
			// on path depth (one key, checked at capture, keeps its operations at
			// depth 2 or less and the descents at depth 3 or more).
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
		HasTenant:           slices.ContainsFunc(resources, func(res *resourceInfo) bool { return res.IsTenant }),
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

// fileHandlerContent renders one @file route's handler for a table or view resource.
func (r *resourceGenerator) fileHandlerContent(res *resourceInfo, file *fileRoute) ([]byte, error) {
	output, err := r.generateTemplateOutput("fileHandler", fileHandlerTemplate, fileHandlerData{
		handlerContentData: handlerContentData{
			ResourcePackage:         r.resource.Package(),
			Resource:                res,
			VirtualResourcesPackage: r.virtual.Package(),
			ApplicationName:         r.applicationName,
			ReceiverName:            r.receiverName,
		},
		File: file,
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
