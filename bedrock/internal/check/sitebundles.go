// sitebundles.go checks each site's browser bundle in the sites layout against where its
// workspace's build writes it. The stack sets the site's bundle variable per service to
// the directory the site's process line in the Procfile sets for development, under the
// image's working directory, and the image copies that directory out of the workspace's
// build stage. Development never serves the bundle through that directory, since ng serve
// serves the browser there, so a Procfile naming a directory the build does not write
// (the workspace's dist when Angular writes dist/<project>) shows nowhere until the
// service answers 404 to every browser path in production, with the API fine beside it.

package check

import (
	"fmt"
	"path"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/render"
)

// SiteBundle is one site's bundle as the check reads it: the directory the Procfile sets
// for development, the one the workspace's build writes, and the one the stack's service
// serves; a line of the report when the Procfile's is the build's, a refusal when the
// Procfile sets none or one the build does not write (Problem set).
type SiteBundle struct {
	// Site is the site's name, Variable its bundle variable (APP_DIST).
	Site     string
	Variable string
	// Procfile is the directory the site's process line in the Procfile sets the
	// variable to, as written there, root-relative; empty when no line sets it.
	Procfile string
	// Output is the root-relative directory the workspace's build writes the bundle to
	// (angular.json's outputPath); empty when angular.json names no single application
	// project to read it from.
	Output string
	// Served is the directory the stack sets the service's variable to, under the
	// image's working directory.
	Served string
	// Problem is what is wrong, empty when the site passes.
	Problem string
}

// scanSiteBundles holds each site's bundle to one directory in the sites layout, from
// what the model read of the Procfile and the workspace's build; nothing in the flat
// layout, whose image sets the bundle variables itself (scanBundles).
func scanSiteBundles(m *derive.Model) []SiteBundle {
	if !m.MultiSite() {
		return nil
	}
	var bundles []SiteBundle
	for i := range m.Sites {
		s := &m.Sites[i]
		if s.Bundle == nil {
			continue
		}
		b := SiteBundle{Site: s.Name, Variable: s.Bundle.Name, Procfile: s.ProcfileBundleDir, Output: s.OutputDir, Served: render.ImageHome + "/" + s.BundleDir}
		b.Problem = b.problem()
		bundles = append(bundles, b)
	}

	return bundles
}

// problem says what is wrong with the site's bundle, or nothing: the Procfile sets no
// directory for the site, or one that is not the directory the build writes, so the
// service would serve a directory without index.html.
func (b *SiteBundle) problem() string {
	switch {
	case b.Procfile == "":
		return fmt.Sprintf("the %s site's process line in the Procfile sets no %s, the directory the site serves its bundle from: the stack sets its service's to %s, the directory the workspace's build writes, and in development the site starts without one (the variable is required); set %s=%s on the %s process line", b.Site, b.Variable, b.Served, b.Variable, b.output(), b.Site)
	case b.Output != "" && path.Clean(b.Procfile) != b.Output:
		return fmt.Sprintf("the %s site's process line in the Procfile sets %s=%s, and its workspace's build writes the bundle to %s (angular.json's outputPath): the stack sets its service's %s to %s, a directory without index.html, so in production every browser path of the site answers 404 while its API answers; set %s=%s on the %s process line, or make the build write to %s", b.Site, b.Variable, b.Procfile, b.Output, b.Variable, b.Served, b.Variable, b.Output, b.Site, b.Procfile)
	default:
		return ""
	}
}

// output is the directory the build writes, as the fix names it: the one read from
// angular.json, else the served directory under the working directory, the one the
// stack has.
func (b *SiteBundle) output() string {
	if b.Output != "" {
		return b.Output
	}

	return b.Served
}

// Passed is the report's line for a site whose bundle is one directory throughout: where
// the service serves it from, as the Procfile sets it and, when angular.json could be
// read, as the build writes it.
func (b *SiteBundle) Passed() string {
	line := fmt.Sprintf("the %s site's service serves its bundle from %s (%s), the directory its process line in the Procfile sets", b.Site, b.Served, b.Variable)
	if b.Output != "" {
		line += " and its workspace's build writes"
	}

	return line
}

// siteBundlesClean reports every site's bundle passing, or none to check.
func (r *Report) siteBundlesClean() bool {
	for i := range r.SiteBundles {
		if r.SiteBundles[i].Problem != "" {
			return false
		}
	}

	return true
}
