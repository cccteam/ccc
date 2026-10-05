package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const configSource = `package config

import (
	"context"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/go-playground/errors/v5"
)

// DataConfiguration is the data level.
type DataConfiguration struct {
	env           *dataConfig
	spannerClient *cloudspanner.Client
	access        *access.Client
}

func NewDataConfiguration(ctx context.Context) (*DataConfiguration, error) {
	env, err := load(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "load()")
	}

	return &DataConfiguration{
		env:           env,
		spannerClient: nil,
		access:        nil,
	}, nil
}
`

const appSource = `package app

// Configurer carries the dependencies.
type Configurer interface {
	Access() string
	ConsoleDist() string
}

type App struct {
	access      string
	consoleDist string
}

func New(cfg Configurer) *App {
	return &App{access: cfg.Access(), consoleDist: cfg.ConsoleDist()}
}
`

func TestGoEdits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		edit    func() ([]byte, error)
		want    string
		wantErr error
	}{
		{
			name: "a struct field",
			edit: func() ([]byte, error) {
				return AddStructField("data.go", []byte(configSource), "DataConfiguration", "tenants tenantRoster")
			},
			want: strings.Replace(configSource, "\taccess        *access.Client\n}", "\taccess        *access.Client\n\ttenants       tenantRoster\n}", 1),
		},
		{
			name: "a struct field the struct declares already",
			edit: func() ([]byte, error) {
				return AddStructField("data.go", []byte(configSource), "DataConfiguration", "// access is the engine.\naccess *access.Client")
			},
			want: configSource,
		},
		{
			name: "a struct the file lacks",
			edit: func() ([]byte, error) {
				return AddStructField("data.go", []byte(configSource), "SiteConfiguration", "x int")
			},
			wantErr: ErrNoAnchor,
		},
		{
			name: "an embedded interface",
			edit: func() ([]byte, error) {
				return AddInterfaceLine("app.go", []byte(appSource), "Configurer", "TenancyConfigurer")
			},
			want: strings.Replace(appSource, "\tConsoleDist() string\n}", "\tConsoleDist() string\n\tTenancyConfigurer\n}", 1),
		},
		{
			name: "an interface method the interface declares already",
			edit: func() ([]byte, error) {
				return AddInterfaceLine("app.go", []byte(appSource), "Configurer", "// Access is the engine.\nAccess() string")
			},
			want: appSource,
		},
		{
			name: "a literal element the literal sets already",
			edit: func() ([]byte, error) {
				return AddLiteralElement("app.go", []byte(appSource), "New", "App", "access: cfg.Access()")
			},
			want: appSource,
		},
		{
			name: "a literal element on a single-line literal",
			edit: func() ([]byte, error) {
				return AddLiteralElement("app.go", []byte(appSource), "New", "App", "tenants: cfg.TenantRoster()")
			},
			want: strings.Replace(appSource, "\treturn &App{access: cfg.Access(), consoleDist: cfg.ConsoleDist()}\n",
				"\treturn &App{access: cfg.Access(), consoleDist: cfg.ConsoleDist(),\n\t\ttenants: cfg.TenantRoster(),\n\t}\n", 1),
		},
		{
			name: "a literal element on a multi-line literal",
			edit: func() ([]byte, error) {
				return AddLiteralElement("data.go", []byte(configSource), "NewDataConfiguration", "DataConfiguration", "tenants: roster")
			},
			want: strings.Replace(configSource, "\t\taccess:        nil,\n\t}, nil", "\t\taccess:        nil,\n\t\ttenants:       roster,\n\t}, nil", 1),
		},
		{
			name: "a function the file lacks",
			edit: func() ([]byte, error) {
				return AddLiteralElement("app.go", []byte(appSource), "NewServer", "App", "x: 1")
			},
			wantErr: ErrNoAnchor,
		},
		{
			name: "a wrapped return",
			edit: func() ([]byte, error) {
				return WrapReturn("data.go", []byte(configSource), "NewDataConfiguration", "DataConfiguration", "conf", "conf.loadTenants(ctx)", `errors.Wrap(err, "loadTenants()")`)
			},
			want: strings.Replace(configSource, "\treturn &DataConfiguration{\n\t\tenv:           env,\n\t\tspannerClient: nil,\n\t\taccess:        nil,\n\t}, nil\n",
				"\tconf := &DataConfiguration{\n\t\tenv:           env,\n\t\tspannerClient: nil,\n\t\taccess:        nil,\n\t}\n\tif err := conf.loadTenants(ctx); err != nil {\n\t\treturn nil, errors.Wrap(err, \"loadTenants()\")\n\t}\n\n\treturn conf, nil\n", 1),
		},
		{
			name: "a return the function lacks",
			edit: func() ([]byte, error) {
				return WrapReturn("app.go", []byte(appSource), "New", "Server", "s", "s.init()", "err")
			},
			wantErr: ErrNoAnchor,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.edit()
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if diff := cmp.Diff(tt.want, string(got)); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStructFieldOfType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		importPath string
		typeName   string
		want       string
	}{
		{name: "the spanner client under its alias", importPath: "cloud.google.com/go/spanner", typeName: "Client", want: "spannerClient"},
		{name: "the access client", importPath: "github.com/cccteam/access", typeName: "Client", want: "access"},
		{name: "a package the file does not import", importPath: "github.com/cccteam/session", typeName: "PasswordAuth"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := StructFieldOfType("data.go", []byte(configSource), "DataConfiguration", tt.importPath, tt.typeName)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("StructFieldOfType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAddImport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, src, path, want string
	}{
		{
			name: "into a block", src: appSource, path: "example.com/beacon/pkg/auth/partners",
			want: strings.Replace(appSource, "package app\n", "package app\n\nimport \"example.com/beacon/pkg/auth/partners\"\n", 1),
		},
		{
			name: "already imported", src: configSource, path: "github.com/cccteam/access",
			want: configSource,
		},
		{
			name: "into an existing block", src: configSource, path: "example.com/beacon/pkg/auth/partners",
			// gofmt sorts the new path into the block.
			want: strings.Replace(configSource, "\tcloudspanner \"cloud.google.com/go/spanner\"\n", "\tcloudspanner \"cloud.google.com/go/spanner\"\n\t\"example.com/beacon/pkg/auth/partners\"\n", 1),
		},
		{
			name: "a single unparenthesized import", src: "package x\n\nimport \"fmt\"\n\nvar _ = fmt.Sprint\n", path: "os",
			want: "package x\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nvar _ = fmt.Sprint\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := AddImport("x.go", []byte(tt.src), tt.path)
			if err != nil {
				t.Fatalf("AddImport() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, string(got)); diff != "" {
				t.Errorf("AddImport() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAddStatementsBeforeConstruction(t *testing.T) {
	t.Parallel()

	const statements = "partnersAuth, err := partners.New(ctx)\nif err != nil {\nreturn nil, err\n}"
	const inserted = "\tpartnersAuth, err := partners.New(ctx)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\n"
	// The constructor as WrapReturn leaves it: the literal is assigned, not returned.
	wrapped := strings.Replace(configSource, "\treturn &DataConfiguration{\n\t\tenv:           env,\n\t\tspannerClient: nil,\n\t\taccess:        nil,\n\t}, nil\n",
		"\tconf := &DataConfiguration{\n\t\tenv:           env,\n\t\tspannerClient: nil,\n\t\taccess:        nil,\n\t}\n\tif err := conf.loadTenants(ctx); err != nil {\n\t\treturn nil, err\n\t}\n\n\treturn conf, nil\n", 1)

	tests := []struct {
		name     string
		rel, src string
		funcName string
		typeName string
		want     string
		wantErr  error
	}{
		{
			name: "before a returned literal",
			rel:  "data.go", src: configSource, funcName: "NewDataConfiguration", typeName: "DataConfiguration",
			want: strings.Replace(configSource, "\treturn &DataConfiguration{", inserted+"\treturn &DataConfiguration{", 1),
		},
		{
			name: "before an assigned literal",
			rel:  "data.go", src: wrapped, funcName: "NewDataConfiguration", typeName: "DataConfiguration",
			want: strings.Replace(wrapped, "\tconf := &DataConfiguration{", inserted+"\tconf := &DataConfiguration{", 1),
		},
		{
			name: "a function the file lacks",
			rel:  "app.go", src: appSource, funcName: "NewServer", typeName: "App",
			wantErr: ErrNoAnchor,
		},
		{
			name: "a literal the function does not build",
			rel:  "app.go", src: appSource, funcName: "New", typeName: "Server",
			wantErr: ErrNoAnchor,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := AddStatementsBeforeConstruction(tt.rel, []byte(tt.src), tt.funcName, tt.typeName, statements)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				if got != nil {
					t.Errorf("an anchor miss returned %d bytes, want nil", len(got))
				}

				return
			}
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if diff := cmp.Diff(tt.want, string(got)); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

const bootstrapSource = `package main

import (
	"context"

	"example.com/acme/beacon/pkg/deploy"
)

func run(ctx context.Context, settings deploy.Settings) error {
	if err := deploy.MigrateSchema(ctx, settings); err != nil {
		return err
	}

	// The development seed: the same data migrations the migrate command applies
	// with -seed in test environments.
	if err := deploy.SeedDevelopmentData(ctx, settings); err != nil {
		return err
	}

	return nil
}

// Close releases the level's clients.
func (c *DataConfiguration) Close() {
	c.staff.Close()
	c.spannerClient.Close()
}
`

// bootstrapRun is the bootstrap's function the cases edit.
const bootstrapRun = "run"

func TestAddStatementsBeforeCall(t *testing.T) {
	t.Parallel()

	const statements = "if err := emptyFileStore(ctx); err != nil {\nreturn err\n}"
	const inserted = "\tif err := emptyFileStore(ctx); err != nil {\n\t\treturn err\n\t}\n\n"

	tests := []struct {
		name     string
		typeName string
		funcName string
		callee   string
		want     string
		wantErr  error
	}{
		{
			name: "before the statement holding the call and the comment introducing it", funcName: bootstrapRun, callee: "deploy.SeedDevelopmentData",
			want: strings.Replace(bootstrapSource, "\t// The development seed:", inserted+"\t// The development seed:", 1),
		},
		{
			name: "before a method's call through the receiver", typeName: "DataConfiguration", funcName: "Close", callee: "c.spannerClient.Close",
			want: strings.Replace(bootstrapSource, "\tc.spannerClient.Close()", inserted+"\tc.spannerClient.Close()", 1),
		},
		{name: "a call the function does not make", funcName: bootstrapRun, callee: "deploy.Reset", wantErr: ErrNoAnchor},
		{name: "a function the file lacks", funcName: "main", callee: bootstrapRun, wantErr: ErrNoAnchor},
		{name: "a method on another type", typeName: "SiteConfiguration", funcName: "Close", callee: "c.spannerClient.Close", wantErr: ErrNoAnchor},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := AddStatementsBeforeCall("main.go", []byte(bootstrapSource), tt.typeName, tt.funcName, tt.callee, statements)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if diff := cmp.Diff(tt.want, string(got)); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestExtendCall(t *testing.T) {
	t.Parallel()

	const src = `package config

func NewDataConfiguration() *DataConfiguration {
	client := resource.NewSpannerClient(spannerClient)
	empty := resource.NewSpannerClient()

	return &DataConfiguration{client: client, empty: empty}
}
`
	tests := []struct {
		name      string
		funcName  string
		callee    string
		arguments string
		want      string
		wantErr   error
	}{
		{
			name: "a call with arguments gains one after a comma", funcName: "NewDataConfiguration", callee: "resource.NewSpannerClient", arguments: "fileStoreOptions(files)...",
			want: strings.Replace(src, "resource.NewSpannerClient(spannerClient)", "resource.NewSpannerClient(spannerClient, fileStoreOptions(files)...)", 1),
		},
		{name: "a call the function does not make", funcName: "NewDataConfiguration", callee: "resource.NewClient", arguments: "x", wantErr: ErrNoAnchor},
		{name: "a function the file lacks", funcName: "New", callee: "resource.NewSpannerClient", arguments: "x", wantErr: ErrNoAnchor},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ExtendCall("data.go", []byte(src), tt.funcName, tt.callee, tt.arguments)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if diff := cmp.Diff(tt.want, string(got)); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestExtendCallEmpty(t *testing.T) {
	t.Parallel()

	const src = "package config\n\nfunc New() *T {\n\treturn build()\n}\n"
	got, err := ExtendCall("t.go", []byte(src), "New", "build", "opts...")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if want := strings.Replace(src, "build()", "build(opts...)", 1); string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestHasCall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		typeName string
		funcName string
		callee   string
		want     bool
		wantErr  error
	}{
		{name: "a call the function makes", funcName: "NewDataConfiguration", callee: "load", want: true},
		{name: "a call the function does not make", funcName: "NewDataConfiguration", callee: "scheduled.FromEnvironment", want: false},
		{name: "a function the file lacks", funcName: "NewSiteConfiguration", callee: "load", wantErr: ErrNoAnchor},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := HasCall("data.go", []byte(configSource), tt.typeName, tt.funcName, tt.callee)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if got != tt.want {
				t.Errorf("HasCall() = %v, want %v", got, tt.want)
			}
		})
	}
}

const methodsSource = `package config

type SiteConfiguration struct {
	scheduler *scheduled.Guard
	jobs      jobs.Starter
}

func (c *SiteConfiguration) Scheduler() *scheduled.Guard { return c.scheduler }

func (c SiteConfiguration) Addr() string { return "" }

func (c *DataConfiguration) Close() {}

func helper() {}
`

func TestMethodNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		typeName string
		want     []string
	}{
		{name: "pointer and value receivers", typeName: "SiteConfiguration", want: []string{"Scheduler", "Addr"}},
		{name: "another type", typeName: "DataConfiguration", want: []string{"Close"}},
		{name: "no methods", typeName: "App"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := MethodNames("site.go", []byte(methodsSource), tt.typeName)
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStructFieldOfValueType(t *testing.T) {
	t.Parallel()

	src := `package config

import (
	"github.com/cccteam/ccc/resource/jobs"
	"github.com/cccteam/ccc/resource/scheduled"
)

type SiteConfiguration struct {
	guard   *scheduled.Guard
	starter jobs.Starter
}
`
	tests := []struct {
		name       string
		importPath string
		typeName   string
		pointer    bool
		want       string
	}{
		{name: "an interface field", importPath: "github.com/cccteam/ccc/resource/jobs", typeName: "Starter", want: "starter"},
		{name: "a pointer field is not a value", importPath: "github.com/cccteam/ccc/resource/scheduled", typeName: "Guard", want: ""},
		{name: "a pointer field through StructFieldOfType", importPath: "github.com/cccteam/ccc/resource/scheduled", typeName: "Guard", pointer: true, want: "guard"},
		{name: "a value field through StructFieldOfType", importPath: "github.com/cccteam/ccc/resource/jobs", typeName: "Starter", pointer: true, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			find := StructFieldOfValueType
			if tt.pointer {
				find = StructFieldOfType
			}
			got, err := find("site.go", []byte(src), "SiteConfiguration", tt.importPath, tt.typeName)
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if got != tt.want {
				t.Errorf("field = %q, want %q", got, tt.want)
			}
		})
	}
}
