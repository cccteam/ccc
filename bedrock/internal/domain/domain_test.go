package domain

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repo is the fixture infrastructure repository: a network layer with a registrant
// contact and one registration, and an environment layer naming the boot project.
const repo = "testdata/repo"

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		domain  string
		wantErr bool
	}{
		{name: "a second-level domain", domain: "example.com"},
		{name: "a subdomain", domain: "lab.example.dev"},
		{name: "digits and inner hyphens", domain: "a1-b2.example.app"},
		{name: "a punycode label", domain: "xn--bcher-kva.example"},
		{name: "a label of 63 characters", domain: strings.Repeat("a", 63) + ".com"},
		{name: "a label of 64 characters", domain: strings.Repeat("a", 64) + ".com", wantErr: true},
		{name: "uppercase", domain: "Example.com", wantErr: true},
		{name: "no top-level domain", domain: "example", wantErr: true},
		{name: "a one-letter top-level domain", domain: "example.c", wantErr: true},
		{name: "digits in the top-level domain", domain: "example.c0m", wantErr: true},
		{name: "a leading hyphen", domain: "-example.com", wantErr: true},
		{name: "a trailing hyphen", domain: "example-.com", wantErr: true},
		{name: "a trailing dot", domain: "example.com.", wantErr: true},
		{name: "an underscore", domain: "exa_mple.com", wantErr: true},
		{name: "a scheme", domain: "https://example.com", wantErr: true},
		{name: "empty", domain: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := Validate(tt.domain)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate(%q) error = %v, wantErr %v", tt.domain, err, tt.wantErr)
			}
		})
	}
}

// fake answers every question with the same parameters and records how it was used.
type fake struct {
	params  *RegisterParameters
	project string
	calls   int
}

// open is the fake's ClientFunc.
func (f *fake) open(_ context.Context, project string) (ParametersClient, error) {
	f.project = project

	return f, nil
}

func (f *fake) RetrieveRegisterParameters(_ context.Context, name string) (*RegisterParameters, error) {
	f.calls++
	p := *f.params
	p.DomainName = name

	return &p, nil
}

// available is Cloud Domains' answer for a domain that can be registered.
func available() *RegisterParameters {
	return &RegisterParameters{
		Availability:     Available,
		Currency:         usd,
		YearlyPrice:      12,
		Notices:          []string{"HSTS_PRELOADED"},
		SupportedPrivacy: []string{"REDACTED_CONTACT_DATA"},
	}
}

// copyRepo copies the fixture repository into a directory the test can edit.
func copyRepo(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "infrastructure")
	if err := os.CopyFS(dir, os.DirFS(repo)); err != nil {
		t.Fatalf("os.CopyFS() error = %v", err)
	}

	return dir
}

// layerFile is the network layer's placement in the copied repository.
func layerFile(dir string) string {
	return filepath.Join(dir, DefaultLayer, tfvarsFile)
}

// editLayer rewrites the network layer's placement through edit.
func editLayer(t *testing.T, dir string, edit func([]byte) []byte) {
	t.Helper()

	data, err := os.ReadFile(layerFile(dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layerFile(dir), edit(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAdd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		req         Request
		params      *RegisterParameters
		mutate      func(t *testing.T, dir string)
		wantProject string
		wantCalls   int
		wantErr     string
		wantFile    []string
	}{
		{
			name:        "an available domain is added, billed to the environment layer's boot project",
			req:         Request{Domain: "example.dev"},
			wantProject: "lab-boot-1234",
			wantCalls:   1,
			wantFile:    []string{`"example.app" = {`, `"example.dev" = {`, "yearly_price_usd = 12", `notices          = ["HSTS_PRELOADED"]`, `email               = "hostmaster@example.com"`},
		},
		{
			name:        "the project asked for is billed",
			req:         Request{Domain: "example.dev", Project: "quota-1"},
			wantProject: "quota-1",
			wantCalls:   1,
		},
		{
			name: "the layer's own boot project comes before the environment layer's",
			req:  Request{Domain: "example.dev"},
			mutate: func(t *testing.T, dir string) {
				t.Helper()

				editLayer(t, dir, func(data []byte) []byte {
					return append(data, []byte("\nboot_project_id = \"lab-net-9\"\n")...)
				})
			},
			wantProject: "lab-net-9",
			wantCalls:   1,
		},
		{
			name: "no project anywhere is refused",
			req:  Request{Domain: "example.dev"},
			mutate: func(t *testing.T, dir string) {
				t.Helper()

				if err := os.RemoveAll(filepath.Join(dir, envLayer)); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: "no project to bill the Cloud Domains call to: pass --project",
		},
		{
			name:    "a domain already placed is refused before asking",
			req:     Request{Domain: "example.app"},
			wantErr: "example.app is already in registrations",
		},
		{
			name: "an empty registrant mailbox is refused before asking",
			req:  Request{Domain: "example.dev"},
			mutate: func(t *testing.T, dir string) {
				t.Helper()

				editLayer(t, dir, func(data []byte) []byte {
					return bytes.Replace(data, []byte(`"hostmaster@example.com"`), []byte(`""`), 1)
				})
			},
			wantErr: "registrant_contact.email in",
		},
		{
			name: "a placement without a registrant contact is refused before asking",
			req:  Request{Domain: "example.dev"},
			mutate: func(t *testing.T, dir string) {
				t.Helper()

				editLayer(t, dir, func([]byte) []byte {
					return []byte("registrations = {}\n")
				})
			},
			wantErr: "registrant_contact.email in",
		},
		{
			name:    "a missing placement is refused",
			req:     Request{Domain: "example.dev", Layer: "9-none"},
			wantErr: "no placement at",
		},
		{
			name:    "a name that is not a bare domain is refused",
			req:     Request{Domain: "Example.dev"},
			wantErr: "a bare lowercase domain",
		},
		{
			name:      "an unavailable domain is refused",
			req:       Request{Domain: "example.dev"},
			params:    &RegisterParameters{Availability: "UNAVAILABLE", Currency: usd},
			wantCalls: 1,
			wantErr:   "example.dev is not available for registration: Cloud Domains reports UNAVAILABLE",
		},
		{
			name:      "a price in another currency is refused",
			req:       Request{Domain: "example.dev"},
			params:    &RegisterParameters{Availability: Available, Currency: "EUR", YearlyPrice: 10},
			wantCalls: 1,
			wantErr:   "example.dev is priced in EUR, not USD",
		},
		{
			name:      "a price with cents is refused",
			req:       Request{Domain: "example.dev"},
			params:    &RegisterParameters{Availability: Available, Currency: usd, YearlyPrice: 12, YearlyPriceNanos: 500000000},
			wantCalls: 1,
			wantErr:   "example.dev costs 12.500000000 USD per year, not a whole number of dollars",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := copyRepo(t)
			if tt.mutate != nil {
				tt.mutate(t, dir)
			}
			before, _ := os.ReadFile(layerFile(dir))
			f := &fake{params: tt.params}
			if f.params == nil {
				f.params = available()
			}
			req := tt.req
			req.Dir = dir
			r, err := Add(context.Background(), f.open, req)
			if f.calls != tt.wantCalls {
				t.Errorf("Cloud Domains was asked %d time(s), want %d", f.calls, tt.wantCalls)
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Add() error = %v, wantErr %q", err, tt.wantErr)
				}
				if after, _ := os.ReadFile(layerFile(dir)); !bytes.Equal(before, after) {
					t.Errorf("Add() edited the placement on a refusal:\n%s", after)
				}

				return
			}
			if err != nil {
				t.Fatalf("Add() error = %v", err)
			}
			if r.Project != tt.wantProject || f.project != tt.wantProject {
				t.Errorf("Add() billed project %q (client opened for %q), want %q", r.Project, f.project, tt.wantProject)
			}
			if r.File != layerFile(dir) || r.Domain != req.Domain || r.Parameters.DomainName != req.Domain {
				t.Errorf("Add() = %+v, want file %s and domain %s", r, layerFile(dir), req.Domain)
			}
			after, err := os.ReadFile(layerFile(dir))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tt.wantFile {
				if !strings.Contains(string(after), want) {
					t.Errorf("the placement lacks %q:\n%s", want, after)
				}
			}
		})
	}
}

func TestResultWrite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		params *RegisterParameters
		want   []string
	}{
		{
			name:   "a notice is glossed",
			params: available(),
			want: []string{
				"example.dev is available at 12 USD per year (asked through project lab-boot-1234).",
				"Notices to acknowledge: HSTS_PRELOADED (HTTPS only; the TLD is on the HSTS preload list).",
				"Contact privacy it supports: REDACTED_CONTACT_DATA.",
				"Added example.dev to 2-net/terraform.tfvars under registrations.",
				"The registrant contact in that file (registrant_contact) must name a mailbox a person reads",
				"Next: commit the change to the values file and open the pull request; the plan on it shows the purchase. The apply registers the domain and points it at the zone.",
			},
		},
		{
			name:   "no notices",
			params: &RegisterParameters{Availability: Available, Currency: usd, YearlyPrice: 9},
			want:   []string{"example.dev is available at 9 USD per year", "It carries no notices."},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			r := &Result{Domain: "example.dev", Project: "lab-boot-1234", File: "2-net/terraform.tfvars", Parameters: tt.params}
			r.Write(&out)
			for _, want := range tt.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("Write() output lacks %q:\n%s", want, out.String())
				}
			}
		})
	}
}
