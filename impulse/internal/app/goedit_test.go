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
			name: "a struct the file lacks",
			edit: func() ([]byte, error) {
				return AddStructField("data.go", []byte(configSource), "ServerConfiguration", "x int")
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
			name: "a literal element on a single-line literal",
			edit: func() ([]byte, error) {
				return AddLiteralElement("app.go", []byte(appSource), "New", "App", "domainVisible: cfg.DomainVisible")
			},
			want: strings.Replace(appSource, "\treturn &App{access: cfg.Access(), consoleDist: cfg.ConsoleDist()}\n",
				"\treturn &App{access: cfg.Access(), consoleDist: cfg.ConsoleDist(),\n\t\tdomainVisible: cfg.DomainVisible,\n\t}\n", 1),
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

func TestAddStatementsBeforeReturn(t *testing.T) {
	t.Parallel()

	got, err := AddStatementsBeforeReturn("data.go", []byte(configSource), "NewDataConfiguration", "DataConfiguration",
		"partnersAuth, err := partners.New(ctx)\nif err != nil {\nreturn nil, err\n}")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	want := strings.Replace(configSource, "\treturn &DataConfiguration{", "\tpartnersAuth, err := partners.New(ctx)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\n\treturn &DataConfiguration{", 1)
	if diff := cmp.Diff(want, string(got)); diff != "" {
		t.Errorf("mismatch (-want +got):\n%s", diff)
	}
	if _, err := AddStatementsBeforeReturn("app.go", []byte(appSource), "New", "Server", "x := 1"); !errors.Is(err, ErrNoAnchor) {
		t.Errorf("error = %v, want ErrNoAnchor", err)
	}
}
