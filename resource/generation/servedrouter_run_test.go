package generation

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// generationImportPath is this package's import path, under which the rendered fixture's
// auth packages are addressed.
const generationImportPath = "github.com/cccteam/ccc/resource/generation"

// Test_servedRouter_generatedTestRuns renders the served router and its test for the
// Lodestar shape, two browser applications under prefixes and a machine outlet, beside a
// hand-written stand-in for the route tables and two auth packages, and runs the rendered
// test with go test: the root redirect, every mount, every route and every refusal hold in
// a compiled router, not only in the template text. A scheduled method rides along, so its
// route under the scheduled prefix and its chain are proven the same way.
//
// The fixture is a package inside this module's testdata, built against the dependency
// versions the module's go.mod declares, as an application would build the generated
// files: outside any workspace (GOWORK=off) and through a scratch copy of go.mod and
// go.sum (-modfile). Nothing else in the module compiles the session library, so go.sum
// carries no sums for its own dependencies; -mod=mod fetches them into the scratch copy
// and the module's files are never written.
func Test_servedRouter_generatedTestRuns(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("compiles and runs the rendered router test with go test")
	}

	// Other tests in the package chdir to the module root (client construction does),
	// so the fixture is placed relative to this source file, not the working directory.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	moduleDir := filepath.Dir(filepath.Dir(thisFile))
	dir, err := os.MkdirTemp(filepath.Join(filepath.Dir(thisFile), "testdata"), "routerfixture-*")
	if err != nil {
		t.Fatalf("os.MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("os.RemoveAll(%s) error = %v", dir, err)
		}
	})
	fixturePath := generationImportPath + "/testdata/" + filepath.Base(dir)
	scratch, err := os.MkdirTemp("", "routerfixture-mod-*")
	if err != nil {
		t.Fatalf("os.MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(scratch); err != nil {
			t.Errorf("os.RemoveAll(%s) error = %v", scratch, err)
		}
	})
	for _, name := range []string{"go.mod", "go.sum"} {
		copyFile(t, filepath.Join(moduleDir, name), filepath.Join(scratch, name))
	}

	// The default outlet answers releases from 1.5.0, the portal only the server's own,
	// and the default outlet serves one stored file, answered at any release.
	outlets := []routerOutlet{
		{name: "default", prefix: "api", servesSessions: true, auth: &outletAuth{importPath: fixturePath + "/crew", flavor: Password}, webApp: "/console", oldestAnswered: "1.5.0", declaredOldest: true},
		{name: "droids", prefix: "droids", apiKey: true},
		{name: "portal", prefix: "portal/api", servesSessions: true, auth: &outletAuth{importPath: fixturePath + "/members", flavor: OIDCGoogle}, webApp: "/portal", oldestAnswered: ThisRelease, declaredOldest: true},
	}
	fileRoutes := map[string][]*generatedRoute{
		"default": {{Path: "/api/widgets/{widgetId}/content", TestURL: "/api/widgets/7/content", HandlerFunc: "WidgetContent", HandlerType: fileHandler}},
	}
	// One scheduled method, mounted under the scheduled prefix behind SchedulerAuth.
	scheduledStruct := fixtureStructs(loadFixture(t, "schedulefixture"))["PruneLogs"]
	r := &resourceGenerator{client: &client{
		genRPCMethods:    true,
		scheduledMethods: []*rpcMethodInfo{{Struct: scheduledStruct, Schedule: &rpcSchedule{Cron: "30 3 * * *", Zone: "America/Denver"}}},
	}}
	r.router = packageDir("pkg/router")
	r.resource = packageDir("pkg/resources")
	data := r.servedRouterData(outlets, nil, fileRoutes)
	if data.RootRedirect != "/console/" || !data.MultiAuth {
		t.Fatalf("RootRedirect = %q, MultiAuth = %v; want /console/ and two auths", data.RootRedirect, data.MultiAuth)
	}

	files := map[string]string{
		generatedGoFileName(servedRouterOutputName):     render(t, r, "servedRouterTemplate", servedRouterTemplate, data),
		generatedGoFileName(servedRouterTestOutputName): render(t, r, "servedRouterTestTemplate", servedRouterTestTemplate, data),
		"routes.go":          routerFixtureRoutes,
		"routes_test.go":     routerFixtureRoutesTest,
		"crew/crew.go":       "package crew\n\n// Name is the auth's name.\nconst Name = \"crew\"\n",
		"members/members.go": "package members\n\n// Name is the auth's name.\nconst Name = \"members\"\n",
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("os.MkdirAll() error = %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("os.WriteFile(%s) error = %v", name, err)
		}
	}

	// The build setup travels in the environment: no workspace, and the scratch module
	// files with the sums -mod=mod adds.
	cmd := exec.CommandContext(t.Context(), "go", "test", "-count=1", "-v", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod -modfile="+filepath.Join(scratch, "go.mod"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test (rendered router): %v\n%s", err, out)
	}
	for _, want := range []string{
		"--- PASS: TestGeneratedRouterRoot/the_root_redirects",
		"--- PASS: TestGeneratedRouterRoot/an_unmatched_path_is_not_found",
		"--- PASS: TestGeneratedRouterWebApps/GET-url-console-generated-router-page-deep-link",
		"--- PASS: TestGeneratedRouterWebApps/GET-url-portal-generated-router-page-deep-link",
		"--- PASS: TestGeneratedRouter/GET-url-api-widgets-7",
		"--- PASS: TestGeneratedRouter/GET-url-portal-api-orders",
		"--- PASS: TestGeneratedRouter/GET-url-droids-beacons",
		"--- PASS: TestGeneratedRouterSessionRoutes/GET-url-portal-api-user-callback",
		"--- PASS: TestGeneratedRouterNotFound/POST-url-droids-user-login",
		"--- PASS: TestGeneratedRouterHooks",
		"--- PASS: TestGeneratedRouterAPIVersion/-api-the_oldest_answered_release",
		"--- PASS: TestGeneratedRouterAPIVersion/-api-a_release_below_the_oldest_answered",
		"--- PASS: TestGeneratedRouterAPIVersion/-api-a_release_above_the_server's",
		"--- PASS: TestGeneratedRouterAPIVersion/-api-no_header",
		"--- PASS: TestGeneratedRouterAPIVersion/-api-a_server_that_is_not_a_release",
		"--- PASS: TestGeneratedRouterAPIVersion/-api-a_session_route_at_any_release_POST-user-login",
		"--- PASS: TestGeneratedRouterAPIVersion/-api-a_stored-file_route_at_any_release_WidgetContent",
		"--- PASS: TestGeneratedRouterAPIVersion/-portal-api-a_release_below_the_oldest_answered",
		"--- PASS: TestGeneratedRouterAPIVersion/-portal-api-a_session_route_at_any_release_GET-user-callback",
		"--- PASS: TestGeneratedRouterAPIVersion/droids_outlet_is_not_checked",
		"--- PASS: TestGeneratedRouterAPIVersion/a_refused_request_never_reaches_the_hook",
		"--- PASS: TestGeneratedRouterScheduled/POST-url-_scheduled-prune-logs",
		"--- PASS: TestGeneratedRouterScheduled/GET-url-_scheduled-prune-logs",
		"--- PASS: TestGeneratedRouterNotFound/GET-url-_scheduled-does-not-exist",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("go test output missing %q:\n%s", want, out)
		}
	}
}

// copyFile copies one file, for the scratch module files the rendered fixture builds with.
func copyFile(t *testing.T, src, dst string) {
	t.Helper()

	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("os.Open(%s) error = %v", src, err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("os.Create(%s) error = %v", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("io.Copy(%s) error = %v", dst, err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("Close(%s) error = %v", dst, err)
	}
}

// routerFixtureRoutes stands in for the route tables the generator emits beside the
// router, reduced to what the router reads: one handler interface and one registration
// function per outlet, with one resource route each.
const routerFixtureRoutes = `package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type GeneratedHandlers interface {
	PermissionDigest() http.HandlerFunc
	UserDomains() http.HandlerFunc
	Widgets() http.HandlerFunc
	WidgetContent() http.HandlerFunc
}

func generatedRoutes(r chi.Router, h GeneratedHandlers) {
	r.Get("/api/permission-digest", h.PermissionDigest())
	r.Get("/api/user-domains", h.UserDomains())
	r.Get("/api/widgets/{widgetId}", h.Widgets())
	r.Get("/api/widgets/{widgetId}/content", h.WidgetContent())
}

type GeneratedDroidsHandlers interface {
	Beacons() http.HandlerFunc
}

func generatedDroidsRoutes(r chi.Router, h GeneratedDroidsHandlers) {
	r.Get("/droids/beacons", h.Beacons())
}

type GeneratedPortalHandlers interface {
	PermissionDigest() http.HandlerFunc
	UserDomains() http.HandlerFunc
	Orders() http.HandlerFunc
}

func generatedPortalRoutes(r chi.Router, h GeneratedPortalHandlers) {
	r.Get("/portal/api/permission-digest", h.PermissionDigest())
	r.Get("/portal/api/user-domains", h.UserDomains())
	r.Get("/portal/api/orders", h.Orders())
}

type GeneratedScheduledHandlers interface {
	PruneLogs() http.HandlerFunc
}

func generatedScheduledRoutes(r chi.Router, h GeneratedScheduledHandlers) {
	r.Post("/_scheduled/prune-logs", h.PruneLogs())
}
`

// routerFixtureRoutesTest stands in for the route tables' test file: the call recorder,
// the route list and the handlers stub the generated router test builds on.
const routerFixtureRoutesTest = `package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type generatedCallRecorder struct {
	handlers   map[string]int
	parameters map[string]map[string]string
}

func newGeneratedCallRecorder() *generatedCallRecorder {
	return &generatedCallRecorder{
		handlers:   make(map[string]int),
		parameters: make(map[string]map[string]string),
	}
}

func (rec *generatedCallRecorder) RecordHandlerCall(name string) http.HandlerFunc {
	return func(_ http.ResponseWriter, r *http.Request) {
		if value := chi.URLParam(r, "widgetId"); value != "" {
			rec.parameters[name] = map[string]string{"widgetId": value}
		}

		rec.handlers[name]++
	}
}

func (rec *generatedCallRecorder) Parameter(name, key string) string {
	return rec.parameters[name][key]
}

type generatedRouterTest struct {
	url         string
	method      string
	handlerFunc string
	parameters  map[string]string
}

func generatedRouterTests() []*generatedRouterTest {
	return []*generatedRouterTest{
		{url: "/api/permission-digest", method: http.MethodGet, handlerFunc: "PermissionDigest"},
		{url: "/api/user-domains", method: http.MethodGet, handlerFunc: "UserDomains"},
		{url: "/api/widgets/7", method: http.MethodGet, handlerFunc: "Widgets", parameters: map[string]string{"widgetId": "7"}},
		{url: "/api/widgets/7/content", method: http.MethodGet, handlerFunc: "WidgetContent", parameters: map[string]string{"widgetId": "7"}},
		{url: "/droids/beacons", method: http.MethodGet, handlerFunc: "Beacons"},
		{url: "/portal/api/permission-digest", method: http.MethodGet, handlerFunc: "PermissionDigest"},
		{url: "/portal/api/user-domains", method: http.MethodGet, handlerFunc: "UserDomains"},
		{url: "/portal/api/orders", method: http.MethodGet, handlerFunc: "Orders"},
	}
}

type generatedHandlersStub struct {
	record func(handlerName string) http.HandlerFunc
}

func newGeneratedHandlersStub(record func(handlerName string) http.HandlerFunc) *generatedHandlersStub {
	return &generatedHandlersStub{record: record}
}

func (s *generatedHandlersStub) PermissionDigest() http.HandlerFunc {
	return s.record("PermissionDigest")
}

func (s *generatedHandlersStub) UserDomains() http.HandlerFunc {
	return s.record("UserDomains")
}

func (s *generatedHandlersStub) Widgets() http.HandlerFunc {
	return s.record("Widgets")
}

func (s *generatedHandlersStub) WidgetContent() http.HandlerFunc {
	return s.record("WidgetContent")
}

func (s *generatedHandlersStub) Beacons() http.HandlerFunc {
	return s.record("Beacons")
}

func (s *generatedHandlersStub) Orders() http.HandlerFunc {
	return s.record("Orders")
}
`
