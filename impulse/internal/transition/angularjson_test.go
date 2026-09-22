package transition

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const smallAngular = `{
  "version": 1,
  "projects": {
    "console": {
      "root": "console",
      "architect": {
        "build": {
          "options": {
            "outputPath": "dist/console",
            "index": "console/src/index.html"
          }
        },
        "serve": {
          "options": {
            "servePath": "/",
            "proxyConfig": "console/proxy.conf.js"
          },
          "configurations": {
            "development": {
              "port": 4300,
              "buildTarget": "console:build:development"
            }
          }
        }
      }
    },
    "other": {
      "root": "other"
    }
  }
}
`

func TestCloneAngularProject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		data     string
		from, to string
		port     int
		want     string
		wantErr  string
	}{
		{
			name: "a copy after the source project",
			data: smallAngular, from: "console", to: "portal", port: 4302,
			want: `{
  "version": 1,
  "projects": {
    "console": {
      "root": "console",
      "architect": {
        "build": {
          "options": {
            "outputPath": "dist/console",
            "index": "console/src/index.html"
          }
        },
        "serve": {
          "options": {
            "servePath": "/",
            "proxyConfig": "console/proxy.conf.js"
          },
          "configurations": {
            "development": {
              "port": 4300,
              "buildTarget": "console:build:development"
            }
          }
        }
      }
    },
    "portal": {
      "root": "portal",
      "architect": {
        "build": {
          "options": {
            "baseHref": "/portal/",
            "outputPath": "dist/portal",
            "index": "portal/src/index.html"
          }
        },
        "serve": {
          "options": {
            "servePath": "/portal",
            "proxyConfig": "portal/proxy.conf.js"
          },
          "configurations": {
            "development": {
              "port": 4302,
              "buildTarget": "portal:build:development"
            }
          }
        }
      }
    },
    "other": {
      "root": "other"
    }
  }
}
`,
		},
		{
			name: "the source project is missing",
			data: smallAngular, from: "admin", to: "portal",
			wantErr: `project "admin" not found`,
		},
		{
			name: "the target project exists",
			data: smallAngular, from: "console", to: "other",
			wantErr: `project "other" already exists`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := cloneAngularProject([]byte(tt.data), tt.from, tt.to, tt.port)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("cloneAngularProject() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("cloneAngularProject() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, string(got)); diff != "" {
				t.Errorf("cloneAngularProject() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRemoveAngularProject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		project string
		want    string
		wantErr string
	}{
		{
			name:    "the last project takes the comma before it",
			project: "other",
			want: strings.Replace(smallAngular, `    },
    "other": {
      "root": "other"
    }
`, "    }\n", 1),
		},
		{
			name:    "the first project takes its own comma",
			project: "console",
			want: `{
  "version": 1,
  "projects": {
    "other": {
      "root": "other"
    }
  }
}
`,
		},
		{name: "a project the workspace lacks", project: "portal", wantErr: `project "portal" not found`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := removeAngularProject([]byte(smallAngular), tt.project)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("removeAngularProject() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("removeAngularProject() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, string(got)); diff != "" {
				t.Errorf("removeAngularProject() diff (-want +got):\n%s", diff)
			}
		})
	}
}
