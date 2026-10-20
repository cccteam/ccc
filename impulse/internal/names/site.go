package names

import (
	"regexp"

	"github.com/go-playground/errors/v5"
)

// siteNameRE is the shape of a site name: one word of lowercase letters and digits
// starting with a letter, the shape of an auth name without the keyword check, since the
// name becomes the site's directory (apps/<name>) and its generator's
// (cmd/generate/<name>generator), never a package name.
var siteNameRE = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// cloudRunNameLimit is the most characters a Cloud Run service name takes.
const cloudRunNameLimit = 49

// maxSiteName bounds the name to what the tightest name it enters leaves it. The name is
// the second segment of every per-site resource name bedrock renders, and the tightest of
// them is the Cloud Run service's, <prefix>-<env>-<region code>-<code>-<site>-app, bounded
// at cloudRunNameLimit. At the organization's maxima (a prefix of 4 characters, an
// environment of 3 (tst, stg, prd), a region code of 3, an application code of 6) the
// other segments and their hyphens take 4+1+3+1+3+1+6+1+1+3 = 24, leaving 25 for the site.
// The default hostname label, <code>-<site>, then fits bedrock's 41-character label rule
// too (6+1+25 = 32). bedrock holds a site to its own placement's budget when it renders;
// this cap is the organization's worst case, so a name impulse accepts is one every
// placement accepts.
const maxSiteName = cloudRunNameLimit - (4 + 1 + 3 + 1 + 3 + 1 + 6 + 1 + 1 + len("app"))

// SiteGuidance is the one-line answer to "what is a good site name".
const SiteGuidance = "one word of lowercase letters and digits starting with a letter, at most 25 characters: console, portal, kiosk"

// ValidateSite checks a site name: its shape, and that it fits the budget the per-site
// resource names leave it (maxSiteName).
func ValidateSite(name string) error {
	if !siteNameRE.MatchString(name) {
		return errors.Newf("site name %q: %s", name, SiteGuidance)
	}
	if len(name) > maxSiteName {
		return errors.Newf("site name %q is %d characters, and %d is the most a site name may take: it is the second segment of every per-site resource name bedrock renders, and the Cloud Run service name <prefix>-<env>-<region code>-<code>-<site>-app fits %d characters with %d left for the site", name, len(name), maxSiteName, cloudRunNameLimit, maxSiteName)
	}

	return nil
}
