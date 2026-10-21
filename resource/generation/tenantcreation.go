package generation

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
)

// The tenant record's creation shape (@tenant(creation: ...)): who may create a tenant,
// declared on the record and separate from who may enter one. An administrator alone is
// the default: the record's create path under a Create grant on it, and no route a
// stranger could reach. Self-service is the other way in: a stranger signs in first,
// through a registration with the open sign-up rule, holds no tenant, and the
// application offers one page, create your tenant; the generated route writes the row
// and seats the person as the tenant's first administrator, in that order. Both is both
// paths. Whatever the shape, an administrator of one tenant never creates another: the
// record is global, and domain roles cannot touch it. The generator renders what the
// shape allows and refuses what it forbids, so no application ships self-service by
// omission.

// tenantCreation is the creation shape, as the annotation names it.
type tenantCreation string

const (
	// tenantCreationAdministrator is the default: an administrator alone creates tenants.
	tenantCreationAdministrator tenantCreation = "administrator"
	// tenantCreationSelfService is self-service alone: the person who becomes the
	// tenant's administrator creates it, and no administrator does.
	tenantCreationSelfService tenantCreation = "self-service"
	// tenantCreationBoth is both paths.
	tenantCreationBoth tenantCreation = "both"
)

// tenantCreationArgKey is the annotation's named argument.
const tenantCreationArgKey = "creation"

// tenantCreationShapes lists the shapes in the order the refusal names them.
var tenantCreationShapes = []tenantCreation{tenantCreationAdministrator, tenantCreationSelfService, tenantCreationBoth}

// AdministratorCreates reports whether an administrator creates tenants under a Create
// grant on the record: the default and both.
func (c tenantCreation) AdministratorCreates() bool {
	return c == "" || c == tenantCreationAdministrator || c == tenantCreationBoth
}

// SelfService reports whether a signed-in person holding no tenant creates one
// through the generated route: self-service and both.
func (c tenantCreation) SelfService() bool {
	return c == tenantCreationSelfService || c == tenantCreationBoth
}

// String spells the shape as the annotation does; the default is spelled out.
func (c tenantCreation) String() string {
	if c == "" {
		return string(tenantCreationAdministrator)
	}

	return string(c)
}

// parseTenantCreation reads @tenant's arguments: none, or creation: naming a shape. A
// positional argument, an unknown key and an unknown shape are refused naming the
// record and the shapes.
func parseTenantCreation(name string, arg genlang.Arg) (tenantCreation, error) {
	if arg.Count() == 0 {
		return tenantCreationAdministrator, nil
	}
	invocations, err := arg.ParseInvocations(&genlang.ArgSpec{Keys: []string{tenantCreationArgKey}})
	if err != nil {
		return "", errors.Wrapf(err, "struct %s: @%s", name, tenantKeyword)
	}
	if len(invocations) != 1 {
		return "", errors.Newf("struct %s: @%s may be declared once", name, tenantKeyword)
	}
	value, ok := invocations[0].Named(tenantCreationArgKey)
	if !ok {
		return tenantCreationAdministrator, nil
	}
	shape := tenantCreation(value)
	for _, known := range tenantCreationShapes {
		if shape == known {
			return shape, nil
		}
	}

	return "", errors.Newf("struct %s: @%s(%s: %s) names no creation shape; the shapes are %s (an administrator alone, the default), %s (the person who becomes its administrator) and %s", name, tenantKeyword, tenantCreationArgKey, value, tenantCreationAdministrator, tenantCreationSelfService, tenantCreationBoth)
}

// tenantCreationRoute is the create-your-tenant route's segment under the outlet
// prefix, named for the record: create-tenant, create-sector.
func tenantCreationRoute(record string) string {
	return "create-" + strcase.ToKebab(record)
}

// tenantCreationHandlerFunc is the handler's name, the generated handlers interface's
// and the application's: CreateTenant, CreateSector.
func tenantCreationHandlerFunc(record string) string {
	return "Create" + record
}

// tenantCreationRolesAccessor is the method the application supplies the administrator
// roles through: TenantAdministratorRoles, the domain roles the person who creates a
// tenant holds in it.
func tenantCreationRolesAccessor(record string) string {
	return record + "AdministratorRoles"
}

// tenantCreationOutlets are the session-serving outlets the record is served on: where
// the create-your-tenant route registers, each serving the same handler.
func (r *resourceGenerator) tenantCreationOutlets(record *resourceInfo) []*routerOutlet {
	var outlets []*routerOutlet
	for _, outlet := range r.allOutlets() {
		if outlet.servesSessions && record.OnOutlet(outlet.name) {
			outlets = append(outlets, outlet)
		}
	}

	return outlets
}

// validateTenantCreation holds the record's creation shape to what the application
// declares around it, once the record and the methods are known. Self-service needs a
// stranger who can sign in: at least one registration with the open sign-up rule
// (OpenSignUp on the method declaration) on the Auth of the outlet serving the record,
// one auth for every session outlet the record is on, since the generated route seats
// the person in that auth's store; and the handler's name must be free, so no RPC
// method is named Create<Record>. An administrator-only record is held to nothing: an
// open registration beside it signs a stranger in to nothing, which is not
// self-service.
func (r *resourceGenerator) validateTenantCreation() error {
	record := r.tenantRecord()
	if record == nil || !record.TenantCreation.SelfService() {
		return nil
	}
	declared := fmt.Sprintf("@%s(%s: %s)", tenantKeyword, tenantCreationArgKey, record.TenantCreation)
	outlets := r.tenantCreationOutlets(record)
	if len(outlets) == 0 {
		return errors.Newf("struct %s: %s on a record no session-serving outlet serves; the create-your-%s route is reached by a signed-in person, so declare the record on an outlet with a session Auth", record.Name(), declared, strcase.ToKebab(record.Name()))
	}
	var auth *outletAuth
	for _, outlet := range outlets {
		switch {
		case outlet.auth == nil:
			return errors.Newf("struct %s: %s, but outlet %q declares no Auth; the create-your-%s route seats the person in the outlet's auth, so declare it (Auth(<package>, ...)) with a registration admitting open sign-up (GoogleMethod(name).OpenSignUp() or AzureMethod(name).OpenSignUp()), or keep the administrator shape", record.Name(), declared, outlet.name, strcase.ToKebab(record.Name()))
		case auth != nil && auth.importPath != outlet.auth.importPath:
			return errors.Newf("struct %s: %s on a record served by outlets bound to different auths (%q and %q); the create-your-%s route seats the person in one auth, so serve the record on the outlets of one (@outlet), or keep the administrator shape", record.Name(), declared, auth.importPath, outlet.auth.importPath, strcase.ToKebab(record.Name()))
		}
		auth = outlet.auth
	}
	open := false
	for _, m := range auth.methods {
		if m.openSignUp {
			open = true
		}
	}
	if !open {
		return errors.Newf("struct %s: %s needs a registration with the open sign-up rule on the %s auth, since a stranger signs in before creating a %s and only open sign-up admits one; declare it on the outlet's Auth (GoogleMethod(name).OpenSignUp() or AzureMethod(name).OpenSignUp(), the registration's Accounts being signin.OpenSignUp), or keep the administrator shape", record.Name(), declared, auth.packageName(), strcase.ToKebab(record.Name()))
	}
	handlerFunc := tenantCreationHandlerFunc(record.Name())
	for _, method := range r.rpcMethods {
		if method.Name() == handlerFunc {
			return errors.Newf("struct %s: %s generates the handler %s, the name of the RPC method %s; rename the method", record.Name(), declared, handlerFunc, method.Name())
		}
	}

	return nil
}

// tenantCreationData feeds the handler package's create-your-tenant file.
type tenantCreationData struct {
	Source              string
	Package             string
	LocalPackageImports string
	ApplicationName     string
	ReceiverName        string
	ResourcePackage     string
	RouterPackage       string
	// Record is the record's struct name, RecordLower the word for one of its rows in
	// prose, Shape the declared creation shape.
	Record      string
	RecordLower string
	Shape       string
	// HandlerFunc is the handler's name, RoutePath the route it is registered at
	// under the default outlet's prefix.
	HandlerFunc string
	RoutePath   string
	// Auth is the outlet auth's Pascal name, AuthType the router package's marker
	// type for it, SurfaceAccessor the application's accessor of the auth's
	// user-management surface, RolesAccessor the application's accessor of the
	// administrator roles.
	Auth            string
	AuthType        string
	SurfaceAccessor string
	RolesAccessor   string
	// Key is the record's key as the request carries it; Fields are the request's
	// fields, the key first; Setters are the create patch's setters, one per field
	// after the key.
	Key     *tenantCreationField
	Fields  []*tenantCreationField
	Setters []string
	// Refusal is the sentence a person who already holds a tenant is answered with.
	Refusal string
	// Directory is the directories record the route declares the creator's directory
	// on, nil where the package declares none.
	Directory *tenantCreationDirectory

	// record is the tenant record the file is rendered from; it scopes import
	// resolution to the types its fields name and is not referenced by the template.
	record *resourceInfo
}

// tenantCreationDirectory is the directories record as the create-your-tenant route
// writes the creator's directory on the new tenant: the record's struct name, the
// setters of its tenant, method and connection columns, and the written row's key as
// the route's undo reads it off the create patch (`directory.ID()`, every key column of
// a compound key in order), the arguments of the record's delete patch constructor.
type tenantCreationDirectory struct {
	Record          string
	TenantField     string
	MethodField     string
	ConnectionField string
	Keys            string
}

func (d *tenantCreationData) typeImports() []fixerImport {
	return resourceTypeImports(nil, d.record)
}

// tenantCreationField is one field of the create-your-tenant request.
type tenantCreationField struct {
	Name string
	Type string
	Wire string
	Tags string
}

// tenantCreationFields are the record's fields as the create-your-tenant request
// carries them: the key, required, then every field the wire may write at create,
// required where the column is. The directory-only flag is not among them: a new
// tenant declares nothing beyond its creator's directory, and whoever holds the
// record's Update grant sets the flag after.
func tenantCreationFields(record *resourceInfo) (key *tenantCreationField, fields []*tenantCreationField, setters []string) {
	for _, field := range record.Fields {
		switch {
		case field.IsPrimaryKey:
			key = &tenantCreationField{Name: field.Name(), Type: field.Type(), Wire: field.WireName(), Tags: field.JSONTag() + ` validate:"required"`}
			fields = append(fields, key)
		case field.IsOutputOnly(), field == record.DirectoryOnlyField:
			// Server-owned, or the declaration's flag: the wire cannot write it here.
		default:
			tags := []string{field.JSONTag()}
			for _, tag := range []string{field.SqltypeTag(), field.NullableTag()} {
				if tag != "" {
					tags = append(tags, tag)
				}
			}
			if field.IsRequired() {
				tags = append(tags, `validate:"required"`)
			}
			f := &tenantCreationField{Name: field.Name(), Type: field.Type(), Wire: field.WireName(), Tags: strings.Join(tags, " ")}
			fields = append(fields, f)
			setters = append(setters, field.Name())
		}
	}

	return key, fields, setters
}

// generateTenantCreation emits the create-your-tenant handler, Create<Record>, into
// the handler package whenever the record's creation shape admits self-service.
func (r *resourceGenerator) generateTenantCreation() error {
	record := r.tenantRecord()
	if record == nil || !record.TenantCreation.SelfService() {
		return nil
	}
	outlets := r.tenantCreationOutlets(record)
	if len(outlets) == 0 || outlets[0].auth == nil {
		return errors.Newf("struct %s: the create-your-%s route has no outlet auth (validateTenantCreation runs first)", record.Name(), strcase.ToKebab(record.Name()))
	}

	begin := time.Now()
	destinationFilePath := filepath.Join(r.handler.Dir(), generatedGoFileName(tenantCreationOutputName))
	if err := r.writeFormattedGoFile(destinationFilePath, "tenantCreationTemplate", tenantCreationTemplate, r.tenantCreationData(record, outlets[0])); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}
	log.Printf("Generated tenant creation file in %s: %s", time.Since(begin), destinationFilePath)

	return nil
}

// tenantCreationData is the handler file's payload for the record on the outlet whose
// auth seats the person: the default outlet's prefix names the route in the doc, and
// every session outlet the record is on registers it.
func (r *resourceGenerator) tenantCreationData(record *resourceInfo, outlet *routerOutlet) *tenantCreationData {
	auth := caser.ToPascal(outlet.auth.packageName())
	key, fields, setters := tenantCreationFields(record)

	return &tenantCreationData{
		Source:              r.resource.Dir(),
		Package:             r.handler.Package(),
		LocalPackageImports: r.localPackageImports(),
		ApplicationName:     r.applicationName,
		ReceiverName:        r.receiverName,
		ResourcePackage:     r.resource.Package(),
		RouterPackage:       r.router.Package(),
		Record:              record.Name(),
		RecordLower:         strings.ToLower(strcase.ToCase(record.Name(), strcase.LowerCase, ' ')),
		Shape:               record.TenantCreation.String(),
		HandlerFunc:         tenantCreationHandlerFunc(record.Name()),
		RoutePath:           fmt.Sprintf("/%s/%s", outlet.prefix, tenantCreationRoute(record.Name())),
		Auth:                auth,
		AuthType:            auth + "Auth",
		SurfaceAccessor:     auth + "UserSurface",
		RolesAccessor:       tenantCreationRolesAccessor(record.Name()),
		Key:                 key,
		Fields:              fields,
		Setters:             setters,
		Refusal:             tenantCreationRefusal(record.Name()),
		Directory:           tenantCreationDirectoryData(record),
		record:              record,
	}
}

// tenantCreationDirectoryData is the directories record as the route writes the
// creator's directory, nil where the record declares none.
func tenantCreationDirectoryData(record *resourceInfo) *tenantCreationDirectory {
	if !record.HasDeclaration() {
		return nil
	}
	directories := record.declaration.Directories
	keys := make([]string, 0, directories.keyCount())
	for _, key := range directories.PrimaryKeys() {
		keys = append(keys, "directory."+key.Name()+"()")
	}

	return &tenantCreationDirectory{
		Record:          directories.Name(),
		TenantField:     directories.DomainBinding.Anchor.Name(),
		MethodField:     directories.DirectoryMethodField.Name(),
		ConnectionField: directories.DirectoryConnectionField.Name(),
		Keys:            strings.Join(keys, ", "),
	}
}

// tenantCreationRefusal is the sentence a person who already holds a tenant is
// answered with: one tenant per person.
func tenantCreationRefusal(record string) string {
	lower := strings.ToLower(strcase.ToCase(record, strcase.LowerCase, ' '))

	return fmt.Sprintf("the session already holds a %s; a person creates one %s and becomes its administrator", lower, lower)
}

// tenantCreationRoutes builds the create-your-tenant route under an outlet prefix: a
// POST, bounded by the router's body limit, with the record's name as its key in the
// route map.
func tenantCreationRoutes(record *resourceInfo, routePrefix string) *generatedRoute {
	path := fmt.Sprintf("/%s/%s", routePrefix, tenantCreationRoute(record.Name()))

	return &generatedRoute{Method: http.MethodPost, Path: path, HandlerFunc: tenantCreationHandlerFunc(record.Name()), TestURL: path}
}

// accumulateTenantCreationRoute adds the create-your-tenant route to every session
// outlet the record is served on, whenever the record's creation shape admits
// self-service. The route joins the dispatch test.
func (r *resourceGenerator) accumulateTenantCreationRoute(outlets []*routerOutlet, outletRoutes []*outletRouteData) (routerTestRoutes []*generatedRoute) {
	record := r.tenantRecord()
	if record == nil || !record.TenantCreation.SelfService() {
		return nil
	}
	for i, outlet := range outlets {
		if !outlet.servesSessions || !record.OnOutlet(outlet.name) {
			continue
		}
		route := tenantCreationRoutes(record, outlet.prefix)
		outletRoutes[i].RoutesMap[route.HandlerFunc] = []*generatedRoute{route}
		routerTestRoutes = append(routerTestRoutes, route)
	}

	return routerTestRoutes
}

// tenantCreationAuthzCases are the matrix's cases for the create-your-tenant route,
// the opposite of a permission gate: a caller holding a grant anywhere holds a tenant
// and is refused, and a caller holding none is admitted past the gate, where the
// minimal body meets its validation (NoGrant renders both halves).
func (r *resourceGenerator) tenantCreationAuthzCases() []authzCase {
	record := r.tenantRecord()
	if record == nil || !record.TenantCreation.SelfService() {
		return nil
	}
	var cases []authzCase
	for _, outlet := range r.tenantCreationOutlets(record) {
		route := tenantCreationRoutes(record, outlet.prefix)
		cases = append(cases, authzCase{
			Name:    authzCaseName(route.HandlerFunc, outlet),
			Method:  httpMethodConst(route.Method),
			URL:     route.TestURL,
			Body:    emptyObjectBody,
			NoGrant: true,
		})
	}

	return cases
}

// tsTenantCreation is the create-your-tenant route as the generated client carries it:
// the route's segment under the API prefix and the body's shape, so a page posts to it
// with the application's HTTP client; the route is no resource and no method of the
// client runtime.
type tsTenantCreation struct {
	Record        string
	RecordLower   string
	Shape         string
	Route         string
	RouteConstant string
	RequestType   string
	Fields        []*tsAPIField
}

// tenantCreationClientData is the create-your-tenant block of a client generated for
// an outlet serving the route; nil where the outlet serves none.
func (t *typescriptGenerator) tenantCreationClientData() *tsTenantCreation {
	var record *resourceInfo
	for _, res := range t.resources {
		if res.IsTenant {
			record = res
		}
	}
	if record == nil || !record.TenantCreation.SelfService() || !record.OnOutlet(t.targetOutlet()) {
		return nil
	}
	out := &tsTenantCreation{
		Record:        record.Name(),
		RecordLower:   strings.ToLower(strcase.ToCase(record.Name(), strcase.LowerCase, ' ')),
		Shape:         record.TenantCreation.String(),
		Route:         tenantCreationRoute(record.Name()),
		RouteConstant: strcase.ToCamel(tenantCreationHandlerFunc(record.Name())) + "Route",
		RequestType:   tenantCreationHandlerFunc(record.Name()),
	}
	for _, field := range record.Fields {
		switch {
		case field.IsPrimaryKey:
			out.Fields = append(out.Fields, newTSAPIField(field, true))
		case field.IsOutputOnly(), field == record.DirectoryOnlyField:
			// Server-owned, or the declaration's flag: the wire cannot write it here.
		default:
			out.Fields = append(out.Fields, newTSAPIField(field, field.IsRequired()))
		}
	}

	return out
}
