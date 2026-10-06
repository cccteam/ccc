package domain

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-playground/errors/v5"
)

// fakeRegistrations is a Cloud Domains holding the registrations of one project; a domain
// it lacks is not registered there, and the domain in fails fails the read.
type fakeRegistrations struct {
	byDomain map[string]RegistrationStatus
	fails    string
}

func (f *fakeRegistrations) Registration(_ context.Context, project, domain string) (*RegistrationStatus, error) {
	if domain == f.fails {
		return nil, errors.New("permission denied on the registration")
	}
	s, ok := f.byDomain[domain]
	if !ok || project != checkProject {
		return &RegistrationStatus{Domain: domain}, nil
	}
	s.Domain, s.Found = domain, true

	return &s, nil
}

func (*fakeRegistrations) Close() error {
	return nil
}

// registeredRepo writes an infrastructure repository whose network layer registers the
// domains, in that order, with the registrant mailbox (none when empty).
func registeredRepo(t *testing.T, mailbox string, domains ...string) string {
	t.Helper()

	var b strings.Builder
	if mailbox != "" {
		b.WriteString("registrant_contact = {\n  email = \"" + mailbox + "\"\n}\n\n")
	}
	b.WriteString("registrations = {\n")
	for _, d := range domains {
		b.WriteString("  \"" + d + "\" = {\n    yearly_price_usd = 14\n    notices          = [\"HSTS_PRELOADED\"]\n  }\n")
	}
	b.WriteString("}\n")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, DefaultLayer), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, DefaultLayer, tfvarsFile), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	return dir
}

// TestRegistrations reads each registration the network layer makes over a fake Cloud
// Domains and holds the report's lines, an unverified registrant mailbox first whatever
// the registrations' order, to their exact text.
func TestRegistrations(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	registered := time.Date(2026, 9, 26, 15, 0, 0, 0, time.UTC)
	nextYear := time.Date(2027, 9, 26, 15, 0, 0, 0, time.UTC)
	const mailbox = "hostmaster@example.com"
	tests := []struct {
		name    string
		mailbox string
		domains []string
		project string
		held    map[string]RegistrationStatus
		fails   string
		noOpen  bool
		// noLayer leaves the repository without a network layer placement.
		noLayer bool
		// wantLines are the report's lines, in order, when wantFirst is set the first
		// line alone.
		wantLines []string
		wantFirst string
		wantErr   string
	}{
		{
			name:    "an active registration: its state and expiry",
			mailbox: mailbox, domains: []string{"example.app"}, project: checkProject,
			held:      map[string]RegistrationStatus{"example.app": {State: "ACTIVE", Created: registered, Expires: nextYear}},
			wantLines: []string{"example.app: ACTIVE, expires on 2027-09-26."},
		},
		{
			name:    "a suspended registration: its state glossed",
			mailbox: mailbox, domains: []string{"example.app"}, project: checkProject,
			held:      map[string]RegistrationStatus{"example.app": {State: "SUSPENDED", Created: registered, Expires: nextYear}},
			wantLines: []string{"example.app: SUSPENDED (the registrar has suspended it, and the domain does not resolve), expires on 2027-09-26."},
		},
		{
			name:    "an unverified registrant mailbox, listed first though its registration is second",
			mailbox: mailbox, domains: []string{"example.app", "example.dev"}, project: checkProject,
			held: map[string]RegistrationStatus{
				"example.app": {State: "ACTIVE", Created: registered, Expires: nextYear},
				"example.dev": {State: "ACTIVE", Created: registered, Expires: nextYear, Issues: []string{IssueUnverifiedEmail}},
			},
			wantLines: []string{
				"In the registrant's mailbox (hostmaster@example.com, registrant_contact.email in 2-net/terraform.tfvars): the registration of example.dev waits on the registrar's verification mail; follow its link by 2026-10-11, fifteen days after the registration on 2026-09-26, or the domain is suspended.",
				"example.app: ACTIVE, expires on 2027-09-26.",
				"example.dev: ACTIVE, expires on 2027-09-26.",
			},
		},
		{
			name:    "an unverified mailbox past its fifteen days: suspended until the link is followed",
			mailbox: mailbox, domains: []string{"example.dev"}, project: checkProject,
			held: map[string]RegistrationStatus{"example.dev": {State: "SUSPENDED", Created: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Expires: nextYear, Issues: []string{IssueUnverifiedEmail}}},
			wantLines: []string{
				"In the registrant's mailbox (hostmaster@example.com, registrant_contact.email in 2-net/terraform.tfvars): the registration of example.dev waits on the registrar's verification mail; it was registered on 2026-09-01 and fifteen days have passed, so the domain is suspended until its link is followed.",
				"example.dev: SUSPENDED (the registrar has suspended it, and the domain does not resolve), expires on 2027-09-26.",
			},
		},
		{
			name:    "an unverified mailbox the placement does not name",
			domains: []string{"example.dev"}, project: checkProject,
			held:      map[string]RegistrationStatus{"example.dev": {State: "REGISTRATION_PENDING", Issues: []string{IssueUnverifiedEmail}}},
			wantFirst: "In the registrant's mailbox (registrant_contact.email in 2-net/terraform.tfvars, which names none): the registration of example.dev waits on the registrar's verification mail; follow its link within fifteen days of the registration, or the domain is suspended.",
		},
		{
			name:    "an expiry near: the days left and the billing account",
			mailbox: mailbox, domains: []string{"example.app"}, project: checkProject,
			held: map[string]RegistrationStatus{"example.app": {State: "ACTIVE", Created: registered, Expires: time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)}},
			wantLines: []string{
				"example.app: ACTIVE, expires on 2026-10-20, in 16 days: it renews automatically while the billing account is active (management_settings in 2-net/domains.tf), so a date this close points at the billing account; if it passes unrenewed, the domain lapses.",
			},
		},
		{
			name:    "an expiry a day away",
			mailbox: mailbox, domains: []string{"example.app"}, project: checkProject,
			held:      map[string]RegistrationStatus{"example.app": {State: "ACTIVE", Expires: now.Add(30 * time.Hour)}},
			wantFirst: "example.app: ACTIVE, expires on 2026-10-05, in 1 day: it renews automatically while the billing account is active (management_settings in 2-net/domains.tf), so a date this close points at the billing account; if it passes unrenewed, the domain lapses.",
		},
		{
			name:    "an issue only support resolves",
			mailbox: mailbox, domains: []string{"example.app"}, project: checkProject,
			held: map[string]RegistrationStatus{"example.app": {State: "SUSPENDED", Expires: nextYear, Issues: []string{IssueContactSupport}}},
			wantLines: []string{
				"example.app: SUSPENDED (the registrar has suspended it, and the domain does not resolve), expires on 2027-09-26.",
				"example.app: Cloud Domains reports an issue only its support resolves (CONTACT_SUPPORT); contact Cloud Domains support from the network project.",
			},
		},
		{
			name:    "a registration the apply has not made yet",
			mailbox: mailbox, domains: []string{"example.app"}, project: checkProject,
			wantLines: []string{"example.app: Cloud Domains holds no registration of it in ex-net-gbl-core-1a2b; the apply of 2-net registers it."},
		},
		{
			name:      "no registrations: nothing is read",
			mailbox:   mailbox,
			noOpen:    true,
			wantLines: []string{"No domain is registered through 2-net (registrations in 2-net/terraform.tfvars lists none)."},
		},
		{
			name:      "no network layer placement: nothing is read",
			noLayer:   true,
			noOpen:    true,
			wantLines: []string{"No domain is registered through 2-net (registrations in 2-net/terraform.tfvars lists none)."},
		},
		{
			name:    "no network project recorded",
			mailbox: mailbox, domains: []string{"example.app"},
			wantErr: "placement.json records no network project (projects.net)",
		},
		{
			name:    "no reader wired",
			mailbox: mailbox, domains: []string{"example.app"}, project: checkProject, noOpen: true,
			wantErr: "no Cloud Domains reader is wired",
		},
		{
			name:    "a read that fails stops the report",
			mailbox: mailbox, domains: []string{"example.app"}, project: checkProject, fails: "example.app",
			wantErr: "permission denied on the registration",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if !tt.noLayer {
				dir = registeredRepo(t, tt.mailbox, tt.domains...)
			}
			var open RegistrationReaderFunc
			if !tt.noOpen {
				open = func(context.Context, string) (RegistrationReader, error) {
					return &fakeRegistrations{byDomain: tt.held, fails: tt.fails}, nil
				}
			}
			r, err := Registrations(context.Background(), open, RegistrationsRequest{Dir: dir, Project: tt.project, Now: now})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Registrations() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Registrations() error = %v", err)
			}
			var out bytes.Buffer
			r.Write(&out)
			lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
			if tt.wantFirst != "" {
				if lines[0] != tt.wantFirst {
					t.Errorf("first line = %q, want %q", lines[0], tt.wantFirst)
				}

				return
			}
			if !slices.Equal(lines, tt.wantLines) {
				t.Errorf("Write() lines =\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(tt.wantLines, "\n"))
			}
		})
	}
}
