// Package domain puts a domain registration into the placement of the network layer, where
// the layer's domains.tf registers it through Cloud Domains. The command asks Cloud Domains
// what registering the name costs and which notices it carries, and writes the answer into
// the layer's terraform.tfvars under registrations; nothing is bought here. The pull
// request's plan then shows the purchase, and the apply registers the domain and points it
// at the zone.
package domain

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	domains "cloud.google.com/go/domains/apiv1beta1"
	"cloud.google.com/go/domains/apiv1beta1/domainspb"
	"github.com/go-playground/errors/v5"
	"google.golang.org/api/option"
)

const (
	// DefaultLayer is the layer whose placement holds the registrations: the network
	// layer, beside the zones the registrations point at.
	DefaultLayer = "2-net"
	// envLayer is the environment layer; its placement names the boot project, which a
	// call is billed to when the network layer's placement names none.
	envLayer = "2-env"
	// tfvarsFile is a layer's placement file.
	tfvarsFile = "terraform.tfvars"
	// bootProjectKey is the placement key naming the boot project.
	bootProjectKey = "boot_project_id"
	// Available is the availability Cloud Domains reports for a domain that can be
	// registered.
	Available = "AVAILABLE"
	// usd is the currency the placement states prices in.
	usd = "USD"
	// hstsPreloaded is the notice on a domain whose top-level domain is on the HSTS
	// preload list.
	hstsPreloaded = "HSTS_PRELOADED"
	// location is the Cloud Domains location under a project.
	location = "global"
)

// domainRE matches a bare lowercase domain: labels of letters, digits and inner hyphens,
// each at most 63 characters, under a top-level domain of letters.
var domainRE = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}$`)

// noticeGloss says in one line what a notice means.
var noticeGloss = map[string]string{
	hstsPreloaded: "HTTPS only; the TLD is on the HSTS preload list",
}

// Validate checks that name is a bare lowercase domain, the only form the layer's
// registrations variable accepts.
func Validate(name string) error {
	if !domainRE.MatchString(name) {
		return errors.Newf("domain %q: a bare lowercase domain, such as example.com", name)
	}

	return nil
}

// RegisterParameters is what Cloud Domains says about registering one domain.
type RegisterParameters struct {
	// DomainName is the name as the registry spells it.
	DomainName string
	// Availability is AVAILABLE when the domain can be registered, else why not
	// (UNAVAILABLE, UNSUPPORTED, UNKNOWN).
	Availability string
	// Currency is the code of the currency the yearly price is in.
	Currency string
	// YearlyPrice is the price of one year in whole units of Currency, and
	// YearlyPriceNanos the fraction of a unit in billionths.
	YearlyPrice      int64
	YearlyPriceNanos int32
	// Notices are the domain notices the registration has to acknowledge.
	Notices []string
	// SupportedPrivacy lists the contact privacy settings the domain supports.
	SupportedPrivacy []string
}

// ParametersClient asks Cloud Domains for a domain's register parameters. The command
// opens one per call through a ClientFunc; tests pass a fake.
type ParametersClient interface {
	// RetrieveRegisterParameters asks about the named domain.
	RetrieveRegisterParameters(ctx context.Context, name string) (*RegisterParameters, error)
}

// ClientFunc opens a ParametersClient whose calls are billed to the project.
// NewCloudDomains is the real one.
type ClientFunc func(ctx context.Context, project string) (ParametersClient, error)

// cloudDomains is the ParametersClient over the Cloud Domains API.
type cloudDomains struct {
	client  *domains.Client
	project string
}

// NewCloudDomains opens the Cloud Domains client with Application Default Credentials,
// billing its calls to the project (the quota project).
func NewCloudDomains(ctx context.Context, project string) (ParametersClient, error) {
	client, err := domains.NewClient(ctx, option.WithQuotaProject(project))
	if err != nil {
		return nil, errors.Wrap(err, "domains.NewClient()")
	}

	return &cloudDomains{client: client, project: project}, nil
}

// RetrieveRegisterParameters asks the API about the named domain.
func (c *cloudDomains) RetrieveRegisterParameters(ctx context.Context, name string) (*RegisterParameters, error) {
	resp, err := c.client.RetrieveRegisterParameters(ctx, &domainspb.RetrieveRegisterParametersRequest{
		DomainName: name,
		Location:   "projects/" + c.project + "/locations/" + location,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "domains.Client.RetrieveRegisterParameters(): %s", name)
	}
	p := resp.GetRegisterParameters()
	price := p.GetYearlyPrice()
	params := &RegisterParameters{
		DomainName:       p.GetDomainName(),
		Availability:     p.GetAvailability().String(),
		Currency:         price.GetCurrencyCode(),
		YearlyPrice:      price.GetUnits(),
		YearlyPriceNanos: price.GetNanos(),
		Notices:          make([]string, 0, len(p.GetDomainNotices())),
		SupportedPrivacy: make([]string, 0, len(p.GetSupportedPrivacy())),
	}
	for _, n := range p.GetDomainNotices() {
		params.Notices = append(params.Notices, n.String())
	}
	for _, s := range p.GetSupportedPrivacy() {
		params.SupportedPrivacy = append(params.SupportedPrivacy, s.String())
	}

	return params, nil
}

// Close releases the API connection.
func (c *cloudDomains) Close() error {
	if err := c.client.Close(); err != nil {
		return errors.Wrap(err, "domains.Client.Close()")
	}

	return nil
}

// Request is what domain add was asked to do.
type Request struct {
	// Domain is the name to register.
	Domain string
	// Dir is the infrastructure repository's root.
	Dir string
	// Layer is the layer whose placement takes the registration; empty means
	// DefaultLayer.
	Layer string
	// Project is the project the Cloud Domains call is billed to; empty means the boot
	// project the placement names.
	Project string
}

// Result is what domain add found and did.
type Result struct {
	// Domain is the name added.
	Domain string
	// Project is the project the call was billed to.
	Project string
	// File is the placement file edited.
	File string
	// Parameters is Cloud Domains' answer about the domain.
	Parameters *RegisterParameters
}

// Add asks Cloud Domains about the domain and adds it to the layer's registrations. It
// refuses, before asking, a name that is not a bare domain, a placement whose registrant
// contact names no mailbox, and a domain the placement already lists; and after asking, a
// domain that is not available or is not priced in whole US dollars.
func Add(ctx context.Context, open ClientFunc, req Request) (*Result, error) {
	if err := Validate(req.Domain); err != nil {
		return nil, err
	}
	layer := req.Layer
	if layer == "" {
		layer = DefaultLayer
	}
	layerDir := filepath.Join(req.Dir, layer)
	file := filepath.Join(layerDir, tfvarsFile)
	src, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.Newf("no placement at %s: --dir is the infrastructure repository's root and --layer the layer whose placement takes the registration", file)
		}

		return nil, errors.Wrap(err, "os.ReadFile()")
	}
	p, err := parsePlacement(src, file)
	if err != nil {
		return nil, err
	}
	if err := p.checkRegistrant(); err != nil {
		return nil, err
	}
	if err := p.checkUnregistered(req.Domain); err != nil {
		return nil, err
	}
	project, err := resolveProject(req, p)
	if err != nil {
		return nil, err
	}
	params, err := ask(ctx, open, project, req.Domain)
	if err != nil {
		return nil, err
	}
	out, err := AddRegistration(src, file, Registration{Domain: req.Domain, YearlyPriceUSD: params.YearlyPrice, Notices: params.Notices})
	if err != nil {
		return nil, err
	}
	if err := writeBack(layerDir, out); err != nil {
		return nil, err
	}

	return &Result{Domain: req.Domain, Project: project, File: file, Parameters: params}, nil
}

// resolveProject picks the project the call is billed to: the one asked for, else the
// boot project the layer's placement names, else the one the environment layer's names.
func resolveProject(req Request, layer *placement) (string, error) {
	if req.Project != "" {
		return req.Project, nil
	}
	id, err := layer.stringValue(bootProjectKey)
	if err != nil {
		return "", err
	}
	if id != "" {
		return id, nil
	}
	envFile := filepath.Join(req.Dir, envLayer, tfvarsFile)
	id, err = bootProject(envFile)
	if err != nil {
		return "", err
	}
	if id != "" {
		return id, nil
	}

	return "", errors.Newf("no project to bill the Cloud Domains call to: pass --project, or set %s in %s or %s", bootProjectKey, layer.file, envFile)
}

// bootProject reads the boot project a placement names, or "" when the file or the key
// is absent.
func bootProject(file string) (string, error) {
	src, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}

		return "", errors.Wrap(err, "os.ReadFile()")
	}
	p, err := parsePlacement(src, file)
	if err != nil {
		return "", err
	}

	return p.stringValue(bootProjectKey)
}

// ask opens a client for the project, asks about the domain, and checks the answer: the
// domain must be available and priced in whole US dollars.
func ask(ctx context.Context, open ClientFunc, project, name string) (*RegisterParameters, error) {
	client, err := open(ctx, project)
	if err != nil {
		return nil, err
	}
	if c, ok := client.(io.Closer); ok {
		defer c.Close()
	}
	params, err := client.RetrieveRegisterParameters(ctx, name)
	if err != nil {
		return nil, err
	}
	if params.Availability != Available {
		return nil, errors.Newf("%s is not available for registration: Cloud Domains reports %s", name, params.Availability)
	}
	if params.Currency != usd {
		return nil, errors.Newf("%s is priced in %s, not %s: the placement holds a yearly price in US dollars", name, params.Currency, usd)
	}
	if params.YearlyPriceNanos != 0 {
		return nil, errors.Newf("%s costs %d.%09d %s per year, not a whole number of dollars: the placement holds whole dollars", name, params.YearlyPrice, params.YearlyPriceNanos, usd)
	}

	return params, nil
}

// writeBack replaces the placement's content, keeping its mode. The write goes through an
// os.Root at the layer directory, as render's writes do.
func writeBack(layerDir string, data []byte) error {
	root, err := os.OpenRoot(layerDir)
	if err != nil {
		return errors.Wrap(err, "os.OpenRoot()")
	}
	defer root.Close()

	info, err := root.Stat(tfvarsFile)
	if err != nil {
		return errors.Wrap(err, "os.Root.Stat()")
	}
	if err := root.WriteFile(tfvarsFile, data, info.Mode().Perm()); err != nil {
		return errors.Wrap(err, "os.Root.WriteFile()")
	}

	return nil
}

// Write prints what was found and what to do next, the way a person reads it.
func (r *Result) Write(w io.Writer) {
	p := r.Parameters
	fmt.Fprintf(w, "%s is %s at %d %s per year (asked through project %s).\n", r.Domain, strings.ToLower(p.Availability), p.YearlyPrice, p.Currency, r.Project)
	if len(p.Notices) == 0 {
		fmt.Fprintln(w, "It carries no notices.")
	} else {
		fmt.Fprintf(w, "Notices to acknowledge: %s.\n", strings.Join(glossed(p.Notices), ", "))
	}
	if len(p.SupportedPrivacy) > 0 {
		fmt.Fprintf(w, "Contact privacy it supports: %s.\n", strings.Join(p.SupportedPrivacy, ", "))
	}
	fmt.Fprintf(w, "Added %s to %s under registrations.\n", r.Domain, r.File)
	fmt.Fprintln(w, "The registrant contact in that file (registrant_contact) must name a mailbox a person reads: the registrar's verification mail goes there, and an unverified domain is suspended.")
	fmt.Fprintln(w, "Next: commit the change to the values file and open the pull request; the plan on it shows the purchase. The apply registers the domain and points it at the zone.")
}

// glossed is each notice with its one-line gloss, when it has one.
func glossed(notices []string) []string {
	out := make([]string, 0, len(notices))
	for _, n := range notices {
		if g, ok := noticeGloss[n]; ok {
			n += " (" + g + ")"
		}
		out = append(out, n)
	}

	return out
}
