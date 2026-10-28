package generation

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/httpio"
	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
)

// The user-management surface the generator serves for every Auth the generated router
// binds (resource.UserSurface): the Auth's users, seats and roles as resources in the
// collection, their routes on the Auth's outlet and on every outlet that manages it,
// the handlers delegating to the library over the surface the application supplies,
// the client descriptor, and the matrix's cases.

// userSurface is one Auth's surface as the generator derives it from the Auth
// declaration.
type userSurface struct {
	importPath string
	// typeName is the marker type the router package declares for the Auth
	// (CrewAuth), accessor the method the application supplies the surface through
	// (CrewUserSurface).
	typeName string
	accessor string
	decl     resource.UserSurfaceDeclaration
	names    resource.UserResourceNames
	// outlets are the names of the outlets serving the surface: the Auth's own first,
	// then the ones that declare Manages on it, in declaration order.
	outlets []string
}

// servedOn reports whether the outlet serves the surface.
func (s *userSurface) servedOn(outlet string) bool {
	return slices.Contains(s.outlets, outlet)
}

// partitions lists the surface's partitions: the global one, and the tenant one where
// the application has tenancy.
func (s *userSurface) partitions() []*resource.UserPartitionNames {
	parts := []*resource.UserPartitionNames{&s.names.Global}
	if s.names.Tenant.Tenanted {
		parts = append(parts, &s.names.Tenant)
	}

	return parts
}

// userSurfaces derives the surfaces of every declared Auth: one per import path, served
// on the outlets that declare the Auth and on those that manage it. Nothing where the
// run generates no routes.
func (r *resourceGenerator) userSurfaces() ([]*userSurface, error) {
	if !r.genRoutes {
		return nil, nil
	}
	tenant, param := "", r.domainRouteParam
	if record := r.tenantRecord(); record != nil {
		tenant = record.Name()
	} else {
		param = ""
	}

	var surfaces []*userSurface
	byPath := make(map[string]*userSurface)
	for _, outlet := range r.allOutlets() {
		if outlet.auth == nil {
			continue
		}
		s, ok := byPath[outlet.auth.importPath]
		if !ok {
			s = &userSurface{importPath: outlet.auth.importPath}
			s.decl = userDeclarationOf(outlet.auth, tenant, param)
			s.typeName = s.decl.Auth + "Auth"
			s.accessor = s.decl.Auth + "UserSurface"
			s.names = s.decl.Names()
			byPath[outlet.auth.importPath] = s
			surfaces = append(surfaces, s)
		}
		s.outlets = append(s.outlets, outlet.name)
	}
	for _, outlet := range r.allOutlets() {
		for _, managed := range outlet.manages {
			s, ok := byPath[managed]
			if !ok {
				return nil, errors.Newf("outlet %q: Manages(%q) names no declared auth; declare it with Auth on the outlet it signs in", outlet.name, managed)
			}
			if outlet.auth != nil && outlet.auth.importPath == managed {
				return nil, errors.Newf("outlet %q: Manages(%q) names the outlet's own auth, whose surface it serves already", outlet.name, managed)
			}
			if !s.servedOn(outlet.name) {
				s.outlets = append(s.outlets, outlet.name)
			}
		}
	}
	for i, s := range surfaces {
		if err := s.decl.Validate(); err != nil {
			return nil, errors.Wrapf(err, "Auth(%q)", s.importPath)
		}
		for _, prior := range surfaces[:i] {
			if prior.decl.Auth == s.decl.Auth {
				return nil, errors.Newf("Auth(%q) and Auth(%q) both name their surface %s: the auth packages' names must differ", prior.importPath, s.importPath, s.decl.Auth)
			}
		}
	}

	return surfaces, nil
}

// userDeclarationOf derives an Auth's declaration from its methods: the password
// method gives the password part, every other registration its name, kind, label and
// what the program declared of its directory.
func userDeclarationOf(auth *outletAuth, tenant, param string) resource.UserSurfaceDeclaration {
	decl := resource.UserSurfaceDeclaration{Auth: caser.ToPascal(assumedPackageName(auth.importPath)), Tenant: tenant, DomainParam: httpio.ParamType(param)}
	for _, m := range auth.methods {
		if m.kind == methodPassword {
			decl.Shape.Password = true
			decl.Shape.UsernameIsEmail = m.usernameIsEmail
			decl.Shape.OpenSignUp = m.openSignUp

			continue
		}
		decl.Shape.Registrations = append(decl.Shape.Registrations, resource.UserRegistration{
			Name:       m.name,
			Kind:       string(m.kind),
			Label:      m.label,
			Invited:    m.invited,
			SyncsRoles: m.syncsRoles,
		})
	}

	return decl
}

// rejectReservedUserNames refuses a struct or manual registration taking a name the
// surfaces hold: the library serves those resources and methods.
func (r *resourceGenerator) rejectReservedUserNames(surfaces []*userSurface) error {
	reserved := make(map[accesstypes.Resource]string)
	for _, s := range surfaces {
		for _, p := range s.partitions() {
			for _, res := range p.Resources() {
				reserved[res] = s.importPath
			}
		}
	}
	refuse := func(name accesstypes.Resource, kind string) error {
		if auth, taken := reserved[name]; taken {
			return errors.Newf("%s %s takes the name of a resource the %s auth's user-management surface serves (Auth(%q)); rename it", kind, name, caser.ToPascal(assumedPackageName(auth)), auth)
		}

		return nil
	}
	errs := make([]error, 0, len(r.resources)+len(r.computedResources)+len(r.rpcMethods)+len(r.manualRegistrations))
	for _, res := range r.resources {
		errs = append(errs, refuse(accesstypes.Resource(r.pluralize(res.Name())), "struct"))
	}
	for _, res := range r.computedResources {
		errs = append(errs, refuse(accesstypes.Resource(r.pluralize(res.Name())), "computed resource"))
	}
	for _, method := range r.rpcMethods {
		errs = append(errs, refuse(accesstypes.Resource(method.Name()), "RPC method"))
	}
	for _, reg := range r.manualRegistrations {
		errs = append(errs, refuse(reg.Resource, "manual registration of"))
	}
	if err := errors.Join(errs...); err != nil {
		return errors.Wrap(err, "the user-management surfaces' names")
	}

	return nil
}

// collectUserRegistrations registers every surface into the collection, the subject
// vocabulary once on the first.
func (r *resourceGenerator) collectUserRegistrations(b *resource.CollectionBuilder) error {
	surfaces, err := r.userSurfaces()
	if err != nil {
		return err
	}
	if err := r.rejectReservedUserNames(surfaces); err != nil {
		return err
	}
	for i, s := range surfaces {
		if err := resource.RegisterUserSurface(b, &s.decl, i == 0); err != nil {
			return errors.Wrapf(err, "registering the user-management surface of Auth(%q)", s.importPath)
		}
	}

	return nil
}

// userRoute is one route of a surface as the router mounts it, with the name the
// handler goes by.
type userRoute struct {
	route *generatedRoute
	// group is the RoutesMap key the route is filed under: the resource or method.
	group string
}

// userRoutes builds a surface's routes under an outlet prefix: each partition's list,
// read and patch of users, seats and roles, and its methods.
func (r *resourceGenerator) userRoutes(s *userSurface, prefix string) []userRoute {
	var routes []userRoute
	for _, p := range s.partitions() {
		routes = append(routes, r.userResourceRoutes(p, prefix, p.Users, p.UsersRoute, []routeTestParam{userTestParam(p.UserParam)}, true)...)
		routes = append(routes, r.userResourceRoutes(p, prefix, p.Seats, p.SeatsRoute, []routeTestParam{userTestParam(p.SeatUserParam), userTestParam(p.SeatRoleParam)}, true)...)
		routes = append(routes, r.userResourceRoutes(p, prefix, p.Roles, p.RolesRoute, []routeTestParam{userTestParam(p.RoleParam)}, false)...)
		routes = append(routes, r.userResourceRoutes(p, prefix, p.Invitations, p.InvitationsRoute, []routeTestParam{userTestParam(p.InvitationParam)}, true)...)
		for _, method := range p.ServedMethods(s.decl.Shape) {
			path, testPath := r.userRoutePaths(prefix, userMethodRoute(p, method), p.Tenanted)
			route := &generatedRoute{Method: http.MethodPost, Path: path, HandlerFunc: string(method), DomainScoped: p.Tenanted, TestURL: testPath}
			route.prependDomainTestParam(r.domainRouteParam)
			routes = append(routes, userRoute{route: route, group: string(method)})
		}
	}

	return routes
}

// userTestParam is a read parameter with the dispatch test's value for it, spelled the
// way the generated resources' are (testCrewUserID).
func userTestParam(param httpio.ParamType) routeTestParam {
	name := string(param)

	return routeTestParam{Key: name, Value: "test" + strings.ToUpper(name[:1]) + name[1:]}
}

// userMethodRoute is a method's route segment in the partition.
func userMethodRoute(p *resource.UserPartitionNames, method accesstypes.Resource) string {
	switch method {
	case p.SetPassword:
		return p.SetPasswordRoute
	case p.UnlinkIdentity:
		return p.UnlinkIdentityRoute
	default:
		return p.ResetFactorsRoute
	}
}

// userResourceRoutes builds one resource's routes: the list, the read with its
// parameters, and the patch where the resource is written.
func (r *resourceGenerator) userResourceRoutes(p *resource.UserPartitionNames, prefix string, res accesstypes.Resource, segment string, params []routeTestParam, written bool) []userRoute {
	base, testBase := r.userRoutePaths(prefix, segment, p.Tenanted)
	list := &generatedRoute{Method: http.MethodGet, Path: base, HandlerFunc: string(res), HandlerType: ListHandler, DomainScoped: p.Tenanted, TestURL: testBase}
	read := &generatedRoute{Method: http.MethodGet, Path: base, HandlerFunc: userSingular(res), HandlerType: ReadHandler, DomainScoped: p.Tenanted, TestURL: testBase, TestParams: params}
	read.appendParamsToPaths()
	routes := []*generatedRoute{list, read}
	if written {
		routes = append(routes, &generatedRoute{Method: http.MethodPatch, Path: base, HandlerFunc: "Patch" + string(res), HandlerType: PatchHandler, DomainScoped: p.Tenanted, TestURL: testBase})
	}
	out := make([]userRoute, 0, len(routes))
	for _, route := range routes {
		route.prependDomainTestParam(r.domainRouteParam)
		out = append(out, userRoute{route: route, group: string(res)})
	}

	return out
}

// userSingular is a surface resource's read handler name: the resource name without
// its plural s (CrewUsers reads as CrewUser).
func userSingular(res accesstypes.Resource) string {
	return strings.TrimSuffix(string(res), "s")
}

// userRoutePaths places a segment under the outlet prefix, and under the domain pair
// for the tenant partition: the mounted path and the dispatch test's URL.
func (r *resourceGenerator) userRoutePaths(prefix, segment string, tenanted bool) (path, testPath string) {
	if tenanted {
		return fmt.Sprintf("/%s/%s/{%s}/%s", prefix, r.domainRouteSegment, r.domainRouteParam, segment),
			fmt.Sprintf("/%s/%s/%s/%s", prefix, r.domainRouteSegment, domainTestValue, segment)
	}
	path = fmt.Sprintf("/%s/%s", prefix, segment)

	return path, path
}

// accumulateUserRoutes adds every surface's routes to the outlets serving it, each route
// joining the dispatch test.
func (r *resourceGenerator) accumulateUserRoutes(outlets []*routerOutlet, outletRoutes []*outletRouteData) ([]*generatedRoute, error) {
	surfaces, err := r.userSurfaces()
	if err != nil {
		return nil, err
	}
	var routerTestRoutes []*generatedRoute
	for _, s := range surfaces {
		for i, outlet := range outlets {
			if !s.servedOn(outlet.name) {
				continue
			}
			for _, ur := range r.userRoutes(s, outlet.prefix) {
				outletRoutes[i].RoutesMap[ur.group] = append(outletRoutes[i].RoutesMap[ur.group], ur.route)
				routerTestRoutes = append(routerTestRoutes, ur.route)
			}
		}
	}

	return routerTestRoutes, nil
}

// userRouteParams lists the read parameters of every surface's routes, in route order:
// the dispatch tests' stubs record a request's parameters by these names.
func (r *resourceGenerator) userRouteParams() ([]string, error) {
	surfaces, err := r.userSurfaces()
	if err != nil {
		return nil, err
	}
	var params []string
	for _, s := range surfaces {
		for _, p := range s.partitions() {
			params = append(params, string(p.UserParam), string(p.SeatUserParam), string(p.SeatRoleParam), string(p.RoleParam), string(p.InvitationParam))
		}
	}

	return params, nil
}

// userNegativeTests are the isolation cases of an outlet: every surface's routes it does
// not serve, addressed under its prefix, must fall through to 404.
func (r *resourceGenerator) userNegativeTests(outlet *routerOutlet) ([]negativeRouterTest, error) {
	surfaces, err := r.userSurfaces()
	if err != nil {
		return nil, err
	}
	var tests []negativeRouterTest
	for _, s := range surfaces {
		if s.servedOn(outlet.name) {
			continue
		}
		for _, ur := range r.userRoutes(s, outlet.prefix) {
			for _, method := range ur.route.TestMethods() {
				tests = append(tests, negativeRouterTest{Method: method, URL: ur.route.TestURL})
			}
		}
	}

	return tests, nil
}

// The placeholder values the matrix addresses the surfaces' rows with.
const (
	userAuthzID   = "00000000-0000-0000-0000-000000000001"
	userAuthzRole = "authz-test-key"
)

// userAuthzCases are the matrix's cases for the surfaces: on every outlet serving one,
// the lists and reads as query pairs, the patches and the methods denied-only, each
// method with its dry run.
func (r *resourceGenerator) userAuthzCases() ([]authzCase, error) {
	surfaces, err := r.userSurfaces()
	if err != nil {
		return nil, err
	}
	var cases []authzCase
	for _, s := range surfaces {
		for _, outlet := range r.allOutlets() {
			if !s.servedOn(outlet.name) {
				continue
			}
			for _, ur := range r.userRoutes(s, outlet.prefix) {
				cases = append(cases, userAuthzCase(ur.route, outlet)...)
			}
		}
	}

	return cases, nil
}

// userAuthzCase renders one route's cases.
func userAuthzCase(route *generatedRoute, outlet *routerOutlet) []authzCase {
	name := authzCaseName(route.HandlerFunc, outlet)
	url := route.TestURL
	switch route.HandlerType {
	case ListHandler:
		return []authzCase{{Name: name, Method: httpMethodConst(route.Method), URL: url, Permission: string(accesstypes.List)}}
	case ReadHandler:
		for _, p := range route.TestParams {
			if p.Key == route.TestParams[0].Key && route.DomainScoped {
				continue
			}
			url = strings.Replace(url, p.Value, userAuthzPlaceholder(p.Key), 1)
		}

		return []authzCase{{Name: name, Method: httpMethodConst(route.Method), URL: url, Permission: string(accesstypes.Read)}}
	case PatchHandler:
		body := fmt.Sprintf(`[{"op":"remove","path":"/%s"}]`, userAuthzID)
		if strings.HasSuffix(route.HandlerFunc, "Seats") {
			body = fmt.Sprintf(`[{"op":"remove","path":"/%s/%s"}]`, userAuthzID, userAuthzRole)
		}

		return []authzCase{{Name: name, Method: httpMethodConst(route.Method), URL: url, Body: body, DeniedOnly: true}}
	default:
		return []authzCase{
			{Name: name, Method: httpMethodConst(route.Method), URL: url, Body: emptyObjectBody, DeniedOnly: true},
			{Name: name + " dry run", Method: httpMethodConst(route.Method), URL: url, Body: emptyObjectBody, Headers: []authzHeader{{Name: resource.DryRunHeader, Value: jsonTrueLiteral}}, DeniedOnly: true},
		}
	}
}

// userAuthzPlaceholder is the matrix's value for a read parameter: an id for a user,
// a key for a role.
func userAuthzPlaceholder(param string) string {
	if strings.HasSuffix(param, "ID") || strings.HasSuffix(param, "User") {
		return userAuthzID
	}

	return userAuthzRole
}

// tenantMethodPrefix is the infix of the library's tenant-partition handler methods
// (TenantUsers, PatchTenantSeats, SetTenantUserPassword).
const tenantMethodPrefix = "Tenant"

// The surface rows' field names and the TypeScript types of their collections, as the
// metadata and the descriptor spell them.
const (
	userFieldUsername    = "username"
	userFieldDisabled    = "disabled"
	userFieldEmail       = "email"
	seatFieldUser        = "user"
	seatFieldRole        = "role"
	roleFieldName        = "name"
	invitationFieldRoles = "roles"
	stringListTSType     = "string[]"
	objectListTSType     = "object[]"
)

// The generated files: the handlers in the handler package, the marker types and
// constructors in the router package.
const (
	usersOutputName        = "users"
	userSurfacesOutputName = "user_surfaces"
)

// usersData feeds the handlers file.
type usersData struct {
	Source              string
	Package             string
	LocalPackageImports string
	ApplicationName     string
	ReceiverName        string
	RouterPackage       string
	Surfaces            []*usersSurfaceData
}

// usersSurfaceData is one surface in the handlers file.
type usersSurfaceData struct {
	Auth       string
	ImportPath string
	TypeName   string
	Accessor   string
	// Handlers are the generated handlers in mount order, each delegating to one
	// method of the surface.
	Handlers []usersHandlerData
}

// usersHandlerData is one generated handler: its name on the application, the
// surface method it delegates to, and what it serves.
type usersHandlerData struct {
	Name    string
	Method  string
	Comment string
}

// usersHandlers lists a surface's handlers.
func usersHandlers(s *userSurface) []usersHandlerData {
	var handlers []usersHandlerData
	for _, p := range s.partitions() {
		where := "the global partition"
		tenant := ""
		if p.Tenanted {
			where = "the route's " + strings.ToLower(s.decl.Tenant)
			tenant = tenantMethodPrefix
		}
		handlers = append(handlers,
			usersHandlerData{Name: string(p.Users), Method: tenant + "Users", Comment: fmt.Sprintf("serves the users of %s: List on %s", where, p.Users)},
			usersHandlerData{Name: userSingular(p.Users), Method: tenant + "User", Comment: fmt.Sprintf("serves one user of %s: Read on %s", where, p.Users)},
			usersHandlerData{Name: "Patch" + string(p.Users), Method: "Patch" + tenant + "Users", Comment: fmt.Sprintf("writes the users of %s through the operations body: Create, Update and Delete on %s", where, p.Users)},
			usersHandlerData{Name: string(p.Seats), Method: tenant + "Seats", Comment: fmt.Sprintf("serves the seats of %s: List on %s", where, p.Seats)},
			usersHandlerData{Name: userSingular(p.Seats), Method: tenant + "Seat", Comment: fmt.Sprintf("serves one seat of %s: Read on %s", where, p.Seats)},
			usersHandlerData{Name: "Patch" + string(p.Seats), Method: "Patch" + tenant + "Seats", Comment: fmt.Sprintf("seats and unseats in %s through the operations body: Create and Delete on %s", where, p.Seats)},
			usersHandlerData{Name: string(p.Roles), Method: tenant + "Roles", Comment: fmt.Sprintf("serves the roles a seat of %s may take: List on %s", where, p.Roles)},
			usersHandlerData{Name: userSingular(p.Roles), Method: tenant + "Role", Comment: fmt.Sprintf("serves one role of %s: Read on %s", where, p.Roles)},
			usersHandlerData{Name: string(p.Invitations), Method: tenant + "Invitations", Comment: fmt.Sprintf("serves the standing invitations into %s: List on %s", where, p.Invitations)},
			usersHandlerData{Name: userSingular(p.Invitations), Method: tenant + "Invitation", Comment: fmt.Sprintf("serves one invitation into %s: Read on %s", where, p.Invitations)},
			usersHandlerData{Name: "Patch" + string(p.Invitations), Method: "Patch" + tenant + "Invitations", Comment: fmt.Sprintf("invites into and revokes from %s through the operations body: Create and Delete on %s", where, p.Invitations)},
		)
		for _, method := range p.ServedMethods(s.decl.Shape) {
			switch method {
			case p.SetPassword:
				handlers = append(handlers, usersHandlerData{Name: string(method), Method: "Set" + tenant + "UserPassword", Comment: fmt.Sprintf("sets a temporary password on a user of %s: Execute on %s, targeting %s", where, method, p.Users)})
			case p.UnlinkIdentity:
				handlers = append(handlers, usersHandlerData{Name: string(method), Method: "Unlink" + tenant + "UserIdentity", Comment: fmt.Sprintf("unlinks an identity from a user of %s: Execute on %s, targeting %s", where, method, p.Users)})
			default:
				handlers = append(handlers, usersHandlerData{Name: string(method), Method: "Reset" + tenant + "UserFactors", Comment: fmt.Sprintf("forgets the browsers a user of %s asked to be remembered, so their next sign-in asks for the second factor: Execute on %s, targeting %s", where, method, p.Users)})
			}
		}
	}

	return handlers
}

// usersDataOf assembles the handlers file's data.
func (r *resourceGenerator) usersDataOf(surfaces []*userSurface) *usersData {
	data := &usersData{
		Source:              r.resource.Dir(),
		Package:             r.handler.Package(),
		LocalPackageImports: r.localPackageImports(),
		ApplicationName:     r.applicationName,
		ReceiverName:        r.receiverName,
		RouterPackage:       r.router.Package(),
	}
	for _, s := range surfaces {
		data.Surfaces = append(data.Surfaces, &usersSurfaceData{Auth: s.decl.Auth, ImportPath: s.importPath, TypeName: s.typeName, Accessor: s.accessor, Handlers: usersHandlers(s)})
	}

	return data
}

// generateUsers writes the handler package's zz_gen_users.go: every surface's handlers
// as delegations to the library over the surface the application supplies. Nothing
// where no Auth is declared.
func (r *resourceGenerator) generateUsers() error {
	surfaces, err := r.userSurfaces()
	if err != nil {
		return err
	}
	if len(surfaces) == 0 {
		return nil
	}
	begin := time.Now()
	destinationFilePath := filepath.Join(r.handler.Dir(), generatedGoFileName(usersOutputName))
	if err := r.writeFormattedGoFile(destinationFilePath, "usersTemplate", usersTemplate, r.usersDataOf(surfaces)); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}
	log.Printf("Generated users file in %s: %s", time.Since(begin), destinationFilePath)

	return nil
}

// userSurfacesData feeds the router package's file.
type userSurfacesData struct {
	Source   string
	Package  string
	Surfaces []*userSurfaceFileData
}

// userSurfaceFileData is one surface's marker type and constructor.
type userSurfaceFileData struct {
	Auth        string
	ImportPath  string
	PackageName string
	TypeName    string
	Accessor    string
	Constructor string
	Tenant      string
	DomainParam string
	Password    bool
	// UsernameIsEmail and OpenSignUp are the password method's flags.
	UsernameIsEmail bool
	OpenSignUp      bool
	Registrations   []resource.UserRegistration
	// OutletList and ResourceList are the serving outlets and the resources, for the
	// type's doc comment.
	OutletList   string
	ResourceList string
}

// generateUserSurfaces writes the router package's zz_gen_user_surfaces.go: a marker
// type per Auth carrying the declaration the generator derived, and the constructor the
// application builds the surface with. Nothing where no Auth is declared.
func (r *resourceGenerator) generateUserSurfaces() error {
	surfaces, err := r.userSurfaces()
	if err != nil {
		return err
	}
	if len(surfaces) == 0 {
		return nil
	}
	data := &userSurfacesData{Source: r.resource.Dir(), Package: r.router.Package()}
	for _, s := range surfaces {
		file := &userSurfaceFileData{
			Auth:            s.decl.Auth,
			ImportPath:      s.importPath,
			PackageName:     assumedPackageName(s.importPath),
			TypeName:        s.typeName,
			Accessor:        s.accessor,
			Constructor:     "New" + s.accessor,
			Tenant:          s.decl.Tenant,
			DomainParam:     string(s.decl.DomainParam),
			Password:        s.decl.Shape.Password,
			UsernameIsEmail: s.decl.Shape.UsernameIsEmail,
			OpenSignUp:      s.decl.Shape.OpenSignUp,
			Registrations:   s.decl.Shape.Registrations,
		}
		var resources []string
		for _, p := range s.partitions() {
			for _, res := range p.Resources() {
				resources = append(resources, string(res))
			}
		}
		file.ResourceList = strings.Join(resources, ", ")
		file.OutletList = outletList(s.outlets)
		data.Surfaces = append(data.Surfaces, file)
	}
	begin := time.Now()
	destinationFilePath := filepath.Join(r.router.Dir(), generatedGoFileName(userSurfacesOutputName))
	if err := r.writeFormattedGoFile(destinationFilePath, "userSurfacesTemplate", userSurfacesTemplate, data); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}
	log.Printf("Generated user surfaces file in %s: %s", time.Since(begin), destinationFilePath)

	return nil
}

// outletList spells the serving outlets for a doc comment.
func outletList(outlets []string) string {
	if len(outlets) == 1 {
		return outlets[0] + " outlet"
	}

	return strings.Join(outlets[:len(outlets)-1], ", ") + " and " + outlets[len(outlets)-1] + " outlets"
}

// The client descriptor and metadata of the surfaces.

// tsUserResource is one surface resource as the metadata and the descriptor carry it.
type tsUserResource struct {
	Name     string
	Property string
	Route    string
	Scope    accesstypes.PermissionScope
	Doc      string
	Fields   []tsUserField
	// Creates, Updates and Deletes say which writes the resource takes.
	Creates, Updates, Deletes bool
	CreateFields              []*tsAPIField
	PatchFields               []*tsAPIField
	Keys                      []*tsAPIField
	Order                     []*tsAPISort
}

// tsUserField is one field of a surface row in the metadata.
type tsUserField struct {
	Name        string
	TsType      string
	DisplayType string
	// Key is the field's position in the key, 0 for a non-key field.
	Key      int
	Required bool
}

// ScopeKind is the resource's scope as the templates spell it.
func (u *tsUserResource) ScopeKind() string {
	return string(u.Scope)
}

// tsUserMethod is one surface method in the metadata and the descriptor.
type tsUserMethod struct {
	Name     string
	Property string
	Route    string
	Scope    accesstypes.PermissionScope
	Fields   []tsUserField
}

// ScopeKind is the method's scope as the templates spell it.
func (m *tsUserMethod) ScopeKind() string {
	return string(m.Scope)
}

// userTSFields are the user row's fields, the same in both partitions.
func userTSFields() []tsUserField {
	return []tsUserField{
		{Name: idIdentifier, TsType: stringTSType, DisplayType: uuidTSType, Key: 1, Required: true},
		{Name: userFieldUsername, TsType: stringTSType, DisplayType: stringTSType, Required: true},
		{Name: userFieldDisabled, TsType: booleanStr, DisplayType: booleanStr, Required: true},
		{Name: "hasPassword", TsType: booleanStr, DisplayType: booleanStr, Required: true},
		{Name: "passwordChangeRequired", TsType: booleanStr, DisplayType: booleanStr, Required: true},
		{Name: "createdAt", TsType: dateTSType, DisplayType: string(displayTypeDate), Required: true},
		{Name: "lastSignInAt", TsType: dateTSType + " | null", DisplayType: string(displayTypeDate)},
		{Name: "organization", TsType: stringTSType + " | null", DisplayType: stringTSType},
		{Name: userFieldEmail, TsType: stringTSType + " | null", DisplayType: stringTSType},
		{Name: "emailVerifiedAt", TsType: dateTSType + " | null", DisplayType: string(displayTypeDate)},
		{Name: "identities", TsType: "UserIdentity[]", DisplayType: objectListTSType, Required: true},
		{Name: "roles", TsType: stringListTSType, DisplayType: stringListTSType, Required: true},
		{Name: "everywhereRoles", TsType: stringListTSType, DisplayType: stringListTSType, Required: true},
		{Name: "rolesSynced", TsType: booleanStr, DisplayType: booleanStr, Required: true},
	}
}

func seatTSFields(tenanted bool) []tsUserField {
	flag := "everyDomain"
	if tenanted {
		flag = "everywhere"
	}

	return []tsUserField{
		{Name: seatFieldUser, TsType: stringTSType, DisplayType: uuidTSType, Key: 1, Required: true},
		{Name: seatFieldRole, TsType: stringTSType, DisplayType: stringTSType, Key: 2, Required: true},
		{Name: userFieldUsername, TsType: stringTSType, DisplayType: stringTSType, Required: true},
		{Name: flag, TsType: booleanStr, DisplayType: booleanStr, Required: true},
	}
}

// invitationTSFields are the invitation row's fields, the same in both partitions.
func invitationTSFields() []tsUserField {
	return []tsUserField{
		{Name: idIdentifier, TsType: stringTSType, DisplayType: uuidTSType, Key: 1, Required: true},
		{Name: userFieldEmail, TsType: stringTSType, DisplayType: stringTSType, Required: true},
		{Name: invitationFieldRoles, TsType: stringListTSType, DisplayType: stringListTSType, Required: true},
		{Name: "invitedBy", TsType: stringTSType + " | null", DisplayType: stringTSType},
		{Name: "createdAt", TsType: dateTSType, DisplayType: string(displayTypeDate), Required: true},
		{Name: "expiresAt", TsType: dateTSType, DisplayType: string(displayTypeDate), Required: true},
	}
}

func roleTSFields(tenanted bool) []tsUserField {
	fields := []tsUserField{{Name: roleFieldName, TsType: stringTSType, DisplayType: stringTSType, Key: 1, Required: true}}
	if !tenanted {
		fields = append(fields, tsUserField{Name: "kind", TsType: "'global' | 'domain'", DisplayType: stringTSType, Required: true})
	}

	return fields
}

// tsUserResources lists a surface's resources for the client.
func tsUserResources(s *userSurface) []*tsUserResource {
	partitions := s.partitions()
	out := make([]*tsUserResource, 0, 4*len(partitions))
	for _, p := range partitions {
		scope := accesstypes.GlobalPermissionScope
		where := "the global partition"
		if p.Tenanted {
			scope = accesstypes.DomainPermissionScope
			where = "one " + strings.ToLower(s.decl.Tenant)
		}
		users := &tsUserResource{
			Name: string(p.Users), Property: strcase.ToCamel(string(p.Users)), Route: p.UsersRoute, Scope: scope,
			Doc:     fmt.Sprintf("A user of the %s auth in %s: the account, its sign-in means and its seats.", s.decl.Auth, where),
			Fields:  userTSFields(),
			Creates: s.decl.Shape.CreatesUsers(), Updates: true, Deletes: true,
			Keys:        []*tsAPIField{{Name: idIdentifier, Type: stringTSType}},
			PatchFields: []*tsAPIField{{Name: userFieldUsername, Type: stringTSType}, {Name: userFieldDisabled, Type: booleanStr}},
			Order:       []*tsAPISort{{Field: userFieldUsername, Direction: sortAsc}},
		}
		// Where the username is the address, no address is entered beside it.
		if !s.decl.Shape.UsernameIsEmail {
			users.PatchFields = append(users.PatchFields, &tsAPIField{Name: userFieldEmail, Type: stringTSType})
		}
		if users.Creates {
			users.CreateFields = []*tsAPIField{{Name: userFieldUsername, Type: stringTSType, Required: true}, {Name: "password", Type: stringTSType}, {Name: userFieldDisabled, Type: booleanStr}}
			if !s.decl.Shape.UsernameIsEmail {
				users.CreateFields = append(users.CreateFields, &tsAPIField{Name: userFieldEmail, Type: stringTSType})
			}
			if p.Tenanted {
				users.CreateFields = append(users.CreateFields, &tsAPIField{Name: seatFieldRole, Type: stringTSType, Required: true})
			}
		}
		seats := &tsUserResource{
			Name: string(p.Seats), Property: strcase.ToCamel(string(p.Seats)), Route: p.SeatsRoute, Scope: scope,
			Doc:     fmt.Sprintf("A seat of the %s auth in %s: a user holding a role.", s.decl.Auth, where),
			Fields:  seatTSFields(p.Tenanted),
			Creates: true, Deletes: true,
			Keys:  []*tsAPIField{{Name: seatFieldUser, Type: stringTSType}, {Name: seatFieldRole, Type: stringTSType}},
			Order: []*tsAPISort{{Field: userFieldUsername, Direction: sortAsc}, {Field: seatFieldRole, Direction: sortAsc}},
		}
		roles := &tsUserResource{
			Name: string(p.Roles), Property: strcase.ToCamel(string(p.Roles)), Route: p.RolesRoute, Scope: scope,
			Doc:    fmt.Sprintf("A role a seat of the %s auth in %s may take.", s.decl.Auth, where),
			Fields: roleTSFields(p.Tenanted),
			Keys:   []*tsAPIField{{Name: roleFieldName, Type: stringTSType}},
			Order:  []*tsAPISort{{Field: roleFieldName, Direction: sortAsc}},
		}
		invitations := &tsUserResource{
			Name: string(p.Invitations), Property: strcase.ToCamel(string(p.Invitations)), Route: p.InvitationsRoute, Scope: scope,
			Doc:     fmt.Sprintf("A standing invitation into %s of the %s auth: an address invited to hold roles, until it is accepted, revoked or expires.", where, s.decl.Auth),
			Fields:  invitationTSFields(),
			Creates: true, Deletes: true,
			Keys:         []*tsAPIField{{Name: idIdentifier, Type: stringTSType}},
			CreateFields: []*tsAPIField{{Name: userFieldEmail, Type: stringTSType, Required: true}, {Name: invitationFieldRoles, Type: stringListTSType, Required: true}},
			Order:        []*tsAPISort{{Field: userFieldEmail, Direction: sortAsc}},
		}
		out = append(out, users, seats, roles, invitations)
	}

	return out
}

// tsUserMethods lists a surface's methods for the client.
func tsUserMethods(s *userSurface) []*tsUserMethod {
	var out []*tsUserMethod
	for _, p := range s.partitions() {
		scope := accesstypes.GlobalPermissionScope
		if p.Tenanted {
			scope = accesstypes.DomainPermissionScope
		}
		for _, method := range p.ServedMethods(s.decl.Shape) {
			m := &tsUserMethod{Name: string(method), Property: strcase.ToCamel(string(method)), Route: userMethodRoute(p, method), Scope: scope}
			switch method {
			case p.SetPassword:
				m.Fields = []tsUserField{{Name: seatFieldUser, TsType: stringTSType, DisplayType: uuidTSType, Required: true}, {Name: "password", TsType: stringTSType, DisplayType: stringTSType, Required: true}}
			case p.UnlinkIdentity:
				m.Fields = []tsUserField{{Name: seatFieldUser, TsType: stringTSType, DisplayType: uuidTSType, Required: true}, {Name: "identity", TsType: stringTSType, DisplayType: uuidTSType, Required: true}}
			default:
				m.Fields = []tsUserField{{Name: seatFieldUser, TsType: stringTSType, DisplayType: uuidTSType, Required: true}}
			}
			out = append(out, m)
		}
	}

	return out
}

// apiSurface is the surface as the descriptor's userSurfaces block carries it: the
// Auth, its label, its shape, and each partition's resource and method names.
func (s *userSurface) apiSurface() *tsAPISurface {
	out := &tsAPISurface{Auth: s.decl.Auth, Label: authLabel(s.decl.Auth), Password: s.decl.Shape.Password, UsernameIsEmail: s.decl.Shape.UsernameIsEmail, OpenSignUp: s.decl.Shape.OpenSignUp}
	for _, reg := range s.decl.Shape.Registrations {
		out.Registrations = append(out.Registrations, &tsAPIRegistration{
			Name:       reg.Name,
			Kind:       reg.Kind,
			Label:      reg.Label,
			Invited:    reg.Invited,
			SyncsRoles: reg.SyncsRoles,
		})
	}
	for _, p := range s.partitions() {
		partition := &tsAPIPartition{
			Scope:       string(accesstypes.GlobalPermissionScope),
			Users:       string(p.Users),
			Seats:       string(p.Seats),
			Roles:       string(p.Roles),
			Invitations: string(p.Invitations),
		}
		if p.Tenanted {
			partition.Scope = string(accesstypes.DomainPermissionScope)
		}
		for _, method := range p.ServedMethods(s.decl.Shape) {
			switch method {
			case p.SetPassword:
				partition.SetPassword = string(method)
			case p.UnlinkIdentity:
				partition.UnlinkIdentity = string(method)
			default:
				partition.ResetFactors = string(method)
			}
		}
		out.Partitions = append(out.Partitions, partition)
	}

	return out
}

// authLabel is the words a page shows for an Auth: its PascalCase name split into
// words, the first kept as written, the rest lowercased unless they are initialisms
// (FleetStaff gives "Fleet staff", HQStaff gives "HQ staff").
func authLabel(auth string) string {
	runes := []rune(auth)
	var words []string
	start := 0
	for i := 1; i < len(runes); i++ {
		afterLower := unicode.IsUpper(runes[i]) && !unicode.IsUpper(runes[i-1])
		beforeLower := unicode.IsUpper(runes[i]) && unicode.IsUpper(runes[i-1]) && i+1 < len(runes) && unicode.IsLower(runes[i+1])
		if afterLower || beforeLower {
			words = append(words, string(runes[start:i]))
			start = i
		}
	}
	words = append(words, string(runes[start:]))
	for i := 1; i < len(words); i++ {
		if !isInitialism(words[i]) {
			words[i] = strings.ToLower(words[i])
		}
	}

	return strings.Join(words, " ")
}

// isInitialism reports whether the word is two or more capitals and nothing else.
func isInitialism(word string) bool {
	if len(word) < 2 {
		return false
	}
	for _, r := range word {
		if !unicode.IsUpper(r) {
			return false
		}
	}

	return true
}

// apiResource is the resource as the descriptor carries it.
func (u *tsUserResource) apiResource() *tsAPIResource {
	ops := []string{opList, opRead}
	if u.Creates {
		ops = append(ops, opCreate)
	}
	if u.Updates {
		ops = append(ops, opPatch)
	}
	if u.Deletes {
		ops = append(ops, opRemove)
	}

	return &tsAPIResource{
		Name:         u.Name,
		Property:     u.Property,
		Route:        u.Route,
		Scope:        u.Scope,
		Keys:         u.Keys,
		Operations:   ops,
		HasCreate:    u.Creates,
		CreateFields: u.CreateFields,
		HasPatch:     u.Updates,
		PatchFields:  u.PatchFields,
		PageDefault:  resource.DefaultPageSize,
		Order:        u.Order,
	}
}

// apiMethod is the method as the descriptor carries it.
func (m *tsUserMethod) apiMethod() *tsAPIMethod {
	return &tsAPIMethod{Name: m.Name, Property: m.Property, Route: m.Route, Scope: m.Scope}
}

// userSurfacesOn lists the surfaces served on the TypeScript target's outlet.
func (t *typescriptGenerator) userSurfacesOn() []*userSurface {
	var served []*userSurface
	for _, s := range t.userSurfaces {
		if s.servedOn(t.targetOutlet()) {
			served = append(served, s)
		}
	}

	return served
}

// tsUserResourcesOn lists the surface resources of the target's outlet.
func (t *typescriptGenerator) tsUserResourcesOn() []*tsUserResource {
	surfaces := t.userSurfacesOn()
	out := make([]*tsUserResource, 0, 3*len(surfaces))
	for _, s := range surfaces {
		out = append(out, tsUserResources(s)...)
	}

	return out
}

// tsUserMethodsOn lists the surface methods of the target's outlet.
func (t *typescriptGenerator) tsUserMethodsOn() []*tsUserMethod {
	surfaces := t.userSurfacesOn()
	out := make([]*tsUserMethod, 0, 2*len(surfaces))
	for _, s := range surfaces {
		out = append(out, tsUserMethods(s)...)
	}

	return out
}
