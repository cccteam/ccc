// spanner.go holds the Spanner instance configurations the model runs on: the
// multi-region configurations bedrock knows, each with the two regions its read-write
// replicas are in, which must be the organization's two Cloud Run regions so that each
// region's service writes to a replica in its own region and either region can lose the
// other; and the regional configuration of an environment's own instance, in the primary
// region, where the primary service runs.

package org

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"
)

// regionalPrefix starts a regional instance configuration's name: regional-<region>.
const regionalPrefix = "regional-"

// SpannerConfig is a multi-region instance configuration and the two regions its
// read-write replicas are in.
type SpannerConfig struct {
	Name      string
	ReadWrite [2]string
}

// The multi-region configurations bedrock knows, and the regions their read-write
// replicas are in.
const (
	configNam6  = "nam6"
	configNam7  = "nam7"
	configNam10 = "nam10"
	configNam11 = "nam11"
	configNam12 = "nam12"
	configNam16 = "nam16"

	usCentral1 = "us-central1"
	usEast1    = "us-east1"
	usEast4    = "us-east4"
	usWest3    = "us-west3"
)

// SpannerConfigs are the multi-region configurations bedrock knows, in the order a
// message lists them.
var SpannerConfigs = []SpannerConfig{
	{Name: configNam10, ReadWrite: [2]string{usCentral1, usWest3}},
	{Name: configNam6, ReadWrite: [2]string{usCentral1, usEast1}},
	{Name: configNam11, ReadWrite: [2]string{usCentral1, usEast1}},
	{Name: configNam7, ReadWrite: [2]string{usCentral1, usEast4}},
	{Name: configNam12, ReadWrite: [2]string{usCentral1, usEast4}},
	{Name: configNam16, ReadWrite: [2]string{usCentral1, usEast4}},
}

// fits reports whether the configuration's read-write regions are the two regions, in
// either order.
func (c SpannerConfig) fits(a, b string) bool {
	return (c.ReadWrite[0] == a && c.ReadWrite[1] == b) || (c.ReadWrite[0] == b && c.ReadWrite[1] == a)
}

// spannerConfig is the configuration of the table by name.
func spannerConfig(name string) (SpannerConfig, bool) {
	for _, c := range SpannerConfigs {
		if c.Name == name {
			return c, true
		}
	}

	return SpannerConfig{}, false
}

// FittingSpannerConfigs are the multi-region configurations whose read-write regions
// are the placement's two regions, in the table's order; none when no configuration
// bedrock knows has them.
func (p *Placement) FittingSpannerConfigs() []string {
	var names []string
	for _, c := range SpannerConfigs {
		if c.fits(p.Regions[0].Name, p.Regions[1].Name) {
			names = append(names, c.Name)
		}
	}

	return names
}

// RegionalSpannerConfig is the regional configuration of an environment's own instance:
// regional-<primary region>, where the primary Cloud Run service runs.
func (p *Placement) RegionalSpannerConfig() string {
	return regionalPrefix + p.Primary().Name
}

// OwnInstanceConfigs are the configurations an environment's own instance may take (2-env's
// spanner_instances): the regional one in the primary region, then the multi-region ones
// that fit the placement's regions.
func (p *Placement) OwnInstanceConfigs() []string {
	return append([]string{p.RegionalSpannerConfig()}, p.FittingSpannerConfigs()...)
}

// OwnInstanceConfigList is OwnInstanceConfigs as an HCL list, for 2-env's validation.
func (p *Placement) OwnInstanceConfigList() string {
	quoted := make([]string, 0, len(p.OwnInstanceConfigs()))
	for _, c := range p.OwnInstanceConfigs() {
		quoted = append(quoted, strconv.Quote(c))
	}

	return "[" + strings.Join(quoted, ", ") + "]"
}

// OwnInstanceConfigsProse names OwnInstanceConfigs for a message: "regional-us-central1 or
// nam10".
func (p *Placement) OwnInstanceConfigsProse() string {
	return orList(p.OwnInstanceConfigs())
}

// OwnInstanceConfigsCode names OwnInstanceConfigs as OwnInstanceConfigsProse does, each
// in backticks, for Markdown.
func (p *Placement) OwnInstanceConfigsCode() string {
	return orList(backticked(p.OwnInstanceConfigs()))
}

// FittingSpannerConfigsCode names FittingSpannerConfigs in backticks, joined with "or",
// for Markdown.
func (p *Placement) FittingSpannerConfigsCode() string {
	return orList(backticked(p.FittingSpannerConfigs()))
}

// orList joins items the way a sentence offers a choice: "a", "a or b", "a, b or c".
func orList(items []string) string {
	if len(items) <= 1 {
		return strings.Join(items, "")
	}

	return strings.Join(items[:len(items)-1], ", ") + " or " + items[len(items)-1]
}

// validateSpanner refuses a shared instance configuration (spanner.config) that is not a
// multi-region configuration bedrock knows, and one whose read-write regions are not the
// placement's two regions; both refusals name the configurations that fit the regions.
func (p *Placement) validateSpanner() error {
	c, known := spannerConfig(p.Spanner.Config)
	switch {
	case !known:
		return errors.Newf("spanner.config %q is not a multi-region configuration bedrock knows (%s): %s", p.Spanner.Config, knownSpannerConfigs(), p.fittingPhrase())
	case !c.fits(p.Regions[0].Name, p.Regions[1].Name):
		return errors.Newf("spanner.config %q has its read-write replicas in %s and %s, not in the placement's regions %s and %s: %s", c.Name, c.ReadWrite[0], c.ReadWrite[1], p.Regions[0].Name, p.Regions[1].Name, p.fittingPhrase())
	}

	return nil
}

// fittingPhrase names the configurations that fit the placement's regions, or says that
// none does.
func (p *Placement) fittingPhrase() string {
	fitting := p.FittingSpannerConfigs()
	if len(fitting) == 0 {
		return fmt.Sprintf("no configuration bedrock knows has its read-write replicas in %s and %s, so the regions are chosen with the configuration", p.Regions[0].Name, p.Regions[1].Name)
	}
	verb := "is"
	if len(fitting) > 1 {
		verb = "are"
	}

	return fmt.Sprintf("the configuration%s whose read-write replicas are in %s and %s %s %s", pluralS(len(fitting)), p.Regions[0].Name, p.Regions[1].Name, verb, prose(fitting))
}

// knownSpannerConfigs lists the table for a message, the configurations grouped by their
// regions: "nam10: us-central1 and us-west3; nam6 and nam11: us-central1 and us-east1; ...".
func knownSpannerConfigs() string {
	var groups []string
	var names []string
	for i, c := range SpannerConfigs {
		names = append(names, c.Name)
		if i+1 < len(SpannerConfigs) && SpannerConfigs[i+1].ReadWrite == c.ReadWrite {
			continue
		}
		groups = append(groups, fmt.Sprintf("%s: %s and %s", prose(names), c.ReadWrite[0], c.ReadWrite[1]))
		names = nil
	}

	return strings.Join(groups, "; ")
}

// pluralS is a noun's plural ending for a count.
func pluralS(n int) string {
	if n == 1 {
		return ""
	}

	return "s"
}
