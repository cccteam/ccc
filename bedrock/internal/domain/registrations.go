// registrations.go reads back the registrations the network layer makes: each domain its
// placement lists under registrations, as Cloud Domains holds it, with its state, its
// expiry date and the issues the registrar raises. The registrar's verification mail goes
// to the registrant mailbox and a person has to follow its link within fifteen days of the
// registration, or the domain is suspended, so a registration waiting on it comes first.

package domain

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	domains "cloud.google.com/go/domains/apiv1beta1"
	"cloud.google.com/go/domains/apiv1beta1/domainspb"
	"github.com/go-playground/errors/v5"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// The registration states the report explains, as Cloud Domains names them.
	stateActive              = "ACTIVE"
	stateRegistrationPending = "REGISTRATION_PENDING"
	stateRegistrationFailed  = "REGISTRATION_FAILED"
	stateSuspended           = "SUSPENDED"
	stateExported            = "EXPORTED"
	// verificationWindow is how long the registrar gives the registrant to follow the
	// verification mail's link before it suspends the domain.
	verificationWindow = 15 * 24 * time.Hour
	// expirySoon is how close an expiry date is before the report says so.
	expirySoon = 30 * 24 * time.Hour
	// day is one day, for counting the days to an expiry.
	day = 24 * time.Hour
	// dateLayout is how the report writes a date.
	dateLayout = "2006-01-02"
)

// The issues Cloud Domains raises on a registration, as it names them: the registrant
// mailbox not verified yet, and a problem only its support resolves.
const (
	IssueUnverifiedEmail = "UNVERIFIED_EMAIL"
	IssueContactSupport  = "CONTACT_SUPPORT"
)

// stateGloss says in a few words what a registration state means, for the states other
// than ACTIVE.
var stateGloss = map[string]string{
	stateRegistrationPending: "the registrar is still registering it",
	stateRegistrationFailed:  "the registration failed and the domain is not registered; the registration's page in Cloud Domains, in the network project, says why",
	stateSuspended:           "the registrar has suspended it, and the domain does not resolve",
	stateExported:            "Cloud Domains no longer manages it (it was transferred to another registrar, or exported)",
}

// RegistrationStatus is one registration as Cloud Domains holds it.
type RegistrationStatus struct {
	// Domain is the registered name.
	Domain string
	// Found is false for a domain the placement lists that the project holds no
	// registration of: the apply of the network layer registers it.
	Found bool
	// State is the registration's state as Cloud Domains names it (ACTIVE, SUSPENDED,
	// REGISTRATION_PENDING).
	State string
	// Created is when the domain was registered, and Expires when the registration
	// runs out unless it is renewed.
	Created time.Time
	Expires time.Time
	// Issues are the issues the registrar raises, as Cloud Domains names them
	// (UNVERIFIED_EMAIL, CONTACT_SUPPORT).
	Issues []string
}

// unverified reports a registration whose registrant mailbox is not verified yet.
func (s RegistrationStatus) unverified() bool {
	return slices.Contains(s.Issues, IssueUnverifiedEmail)
}

// RegistrationReader reads registrations. NewRegistrationReader is the real one, over
// Cloud Domains; tests pass a fake.
type RegistrationReader interface {
	// Registration is the domain's registration in the project; Found is false when the
	// project holds none.
	Registration(ctx context.Context, project, domain string) (*RegistrationStatus, error)
	// Close releases the connection.
	Close() error
}

// RegistrationReaderFunc opens a RegistrationReader whose calls are billed to the
// project.
type RegistrationReaderFunc func(ctx context.Context, project string) (RegistrationReader, error)

// registrations is the RegistrationReader over the Cloud Domains API.
type registrations struct {
	client *domains.Client
}

// NewRegistrationReader opens the Cloud Domains client with Application Default
// Credentials, billing its calls to the project (the quota project): the network project,
// where the registrations are and the layer enables the API.
func NewRegistrationReader(ctx context.Context, project string) (RegistrationReader, error) {
	client, err := domains.NewClient(ctx, option.WithQuotaProject(project))
	if err != nil {
		return nil, errors.Wrap(err, "domains.NewClient()")
	}

	return &registrations{client: client}, nil
}

// Registration reads the domain's registration in the project (registrations.get).
func (r *registrations) Registration(ctx context.Context, project, domain string) (*RegistrationStatus, error) {
	reg, err := r.client.GetRegistration(ctx, &domainspb.GetRegistrationRequest{
		Name: "projects/" + project + "/locations/" + location + "/registrations/" + domain,
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return &RegistrationStatus{Domain: domain}, nil
		}

		return nil, errors.Wrapf(err, "domains.Client.GetRegistration(): %s in %s", domain, project)
	}
	s := &RegistrationStatus{Domain: domain, Found: true, State: reg.GetState().String(), Issues: make([]string, 0, len(reg.GetIssues()))}
	if t := reg.GetCreateTime(); t != nil {
		s.Created = t.AsTime()
	}
	if t := reg.GetExpireTime(); t != nil {
		s.Expires = t.AsTime()
	}
	for _, issue := range reg.GetIssues() {
		s.Issues = append(s.Issues, issue.String())
	}

	return s, nil
}

// Close releases the API connection.
func (r *registrations) Close() error {
	if err := r.client.Close(); err != nil {
		return errors.Wrap(err, "domains.Client.Close()")
	}

	return nil
}

// RegistrationsRequest is what the registration report reads.
type RegistrationsRequest struct {
	// Dir is the infrastructure repository's root, whose network layer placement lists
	// the registrations and names the registrant mailbox.
	Dir string
	// Project is the network project, where the registrations are; empty while the
	// organization's placement records none.
	Project string
	// Now is the time the expiry dates and the verification deadline are measured from.
	Now time.Time
}

// RegistrationReport is each registration the network layer makes, as Cloud Domains
// holds it.
type RegistrationReport struct {
	// Project is the network project read.
	Project string
	// Mailbox is the registrant mailbox the placement names, empty when it names none.
	Mailbox string
	// Registrations are the registrations in the placement's order.
	Registrations []RegistrationStatus
	// Now is the time the report measures from.
	Now time.Time
}

// Registrations reads each registration the network layer's placement lists through
// Cloud Domains. A repository without that placement, or one listing none, has none to
// read, and nothing is opened. It is refused, before any read, when the placement does
// not parse, when the organization's placement records no network project, and when no
// reader is wired; a reader that does not open or a read that fails stops it too.
func Registrations(ctx context.Context, open RegistrationReaderFunc, req RegistrationsRequest) (*RegistrationReport, error) {
	names, mailbox, err := registeredDomains(req.Dir)
	if err != nil {
		return nil, err
	}
	r := &RegistrationReport{Project: req.Project, Mailbox: mailbox, Now: req.Now}
	if len(names) == 0 {
		return r, nil
	}
	if req.Project == "" {
		return nil, errors.New("placement.json records no network project (projects.net), where the registrations are")
	}
	if open == nil {
		return nil, errors.New("no Cloud Domains reader is wired")
	}
	reader, err := open(ctx, req.Project)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	for _, name := range names {
		s, err := reader.Registration(ctx, req.Project, name)
		if err != nil {
			return nil, err
		}
		r.Registrations = append(r.Registrations, *s)
	}

	return r, nil
}

// registeredDomains lists the domains the network layer's placement registers, in file
// order, and the registrant mailbox it names; a repository without that placement has
// none.
func registeredDomains(dir string) (names []string, mailbox string, err error) {
	file := filepath.Join(dir, DefaultLayer, tfvarsFile)
	src, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", nil
		}

		return nil, "", errors.Wrap(err, "os.ReadFile()")
	}
	p, err := parsePlacement(src, file)
	if err != nil {
		return nil, "", err
	}
	names, err = p.registrations()
	if err != nil {
		return nil, "", err
	}
	mailbox, err = p.registrantEmail()
	if err != nil {
		return nil, "", err
	}

	return names, mailbox, nil
}

// Write prints the report the way a person reads it: first each registration that waits
// on the registrant mailbox's verification, then each registration's state and expiry
// date, with the issues the registrar raises on it.
func (r *RegistrationReport) Write(w io.Writer) {
	if len(r.Registrations) == 0 {
		fmt.Fprintf(w, "No domain is registered through %s (registrations in %s lists none).\n", DefaultLayer, netValues)

		return
	}
	for _, s := range r.Registrations {
		if s.unverified() {
			r.writeUnverified(w, s)
		}
	}
	for _, s := range r.Registrations {
		r.writeState(w, s)
	}
}

// writeUnverified names the mailbox the registrar's verification mail went to and the
// date its link must be followed by.
func (r *RegistrationReport) writeUnverified(w io.Writer, s RegistrationStatus) {
	mailbox := fmt.Sprintf("%s, %s.%s in %s", r.Mailbox, registrantKey, emailKey, netValues)
	if r.Mailbox == "" {
		mailbox = fmt.Sprintf("%s.%s in %s, which names none", registrantKey, emailKey, netValues)
	}
	when := "follow its link within fifteen days of the registration, or the domain is suspended"
	if !s.Created.IsZero() {
		deadline := s.Created.Add(verificationWindow)
		when = fmt.Sprintf("follow its link by %s, fifteen days after the registration on %s, or the domain is suspended", deadline.Format(dateLayout), s.Created.Format(dateLayout))
		if !r.Now.Before(deadline) {
			when = fmt.Sprintf("it was registered on %s and fifteen days have passed, so the domain is suspended until its link is followed", s.Created.Format(dateLayout))
		}
	}
	fmt.Fprintf(w, "In the registrant's mailbox (%s): the registration of %s waits on the registrar's verification mail; %s.\n", mailbox, s.Domain, when)
}

// writeState prints one registration's state and expiry date, then the issues other than
// the mailbox's.
func (r *RegistrationReport) writeState(w io.Writer, s RegistrationStatus) {
	if !s.Found {
		fmt.Fprintf(w, "%s: Cloud Domains holds no registration of it in %s; the apply of %s registers it.\n", s.Domain, r.Project, DefaultLayer)

		return
	}
	line := s.Domain + ": " + s.State
	if gloss, ok := stateGloss[s.State]; ok {
		line += " (" + gloss + ")"
	}
	if !s.Expires.IsZero() {
		line += ", expires on " + s.Expires.Format(dateLayout)
		if left := s.Expires.Sub(r.Now); s.State == stateActive && left > 0 && left < expirySoon {
			line += fmt.Sprintf(", in %s: it renews automatically while the billing account is active (management_settings in %s/domains.tf), so a date this close points at the billing account; if it passes unrenewed, the domain lapses", daysPhrase(left), DefaultLayer)
		}
	}
	fmt.Fprintln(w, line+".")
	for _, issue := range s.Issues {
		switch issue {
		case IssueUnverifiedEmail:
			continue
		case IssueContactSupport:
			fmt.Fprintf(w, "%s: Cloud Domains reports an issue only its support resolves (%s); contact Cloud Domains support from the network project.\n", s.Domain, issue)
		default:
			fmt.Fprintf(w, "%s: Cloud Domains reports the issue %s.\n", s.Domain, issue)
		}
	}
}

// daysPhrase says how many whole days a span is: "less than a day", "1 day", "12 days".
func daysPhrase(d time.Duration) string {
	days := int(d / day)
	switch {
	case d < day:
		return "less than a day"
	case days == 1:
		return "1 day"
	default:
		return fmt.Sprintf("%d days", days)
	}
}
