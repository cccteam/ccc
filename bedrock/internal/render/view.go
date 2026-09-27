// view.go prepares what the templates read: the model, the placement, and the phrases
// and aligned blocks that are easier to compute once than to spell in a template.

package render

import (
	"fmt"
	"hash/fnv"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// view is the data every template executes against.
type view struct {
	*derive.Model
	P *derive.Placement
	// RepoFullName is the repository as GitHub names it, owner/name, from the module
	// path; a placeholder when the module path names no GitHub repository.
	RepoFullName string
	// SweepMinute is the minute of the hour the application's sweep runs at, derived
	// from the application code, so that many applications' sweeps in one
	// environment do not all queue at the top of the hour.
	SweepMinute int

	// Prefix is the placement's naming prefix.
	Prefix string
	// Integration and Production are the first and last environments.
	Integration string
	Production  string
	// SharedInstanceEnvs names the environments above integration: "stg and prd";
	// SharedInstanceEnvsWrapped is the same with the last on its own README line.
	SharedInstanceEnvs        string
	SharedInstanceEnvsWrapped string
	// EnvsOr, EnvsComma, EnvsBacktickOr, EnvList spell the environments: "tst, stg, or
	// prd", "tst, stg, prd", "`tst`, `stg`, or `prd`", ["tst", "stg", "prd"].
	EnvsOr         string
	EnvsComma      string
	EnvsBacktickOr string
	EnvList        string
	// ApprovalList is the HCL list of the environments a release waits for approval
	// in; ApprovalsProse spells them ("stg and prd", or "no environment").
	// PreviousEnvMap is the HCL map from each environment to the one before it.
	ApprovalList   string
	ApprovalsProse string
	PreviousEnvMap string
	// EnvCountWord is the number of environments as a word.
	EnvCountWord string
	// EmptyEnvStrings and EmptyEnvMaps are the per-environment default blocks, env = ""
	// and env = {}, indented four spaces for a variable block; the Tfvars forms are the
	// same at the top level of terraform.tfvars, indented two.
	EmptyEnvStrings  string
	EmptyEnvMaps     string
	TfvarsEnvStrings string
	TfvarsEnvMaps    string
	// RegionCodes joins the region codes with |; RegionsSub is the _REGIONS
	// substitution; PrimaryCode is the primary region's code.
	RegionCodes string
	RegionsSub  string
	PrimaryCode string

	// Auth is the auth the site binds to.
	Auth *derive.Auth
	// AuthVar is the stem of the auth's placement variables: <auth>_oidc.
	AuthVar string
	// RoutesDir is the directory of the file registering the callback.
	RoutesDir string
	// The variables by role, for the templates that name them.
	ServiceName      *derive.Variable
	LoggingProject   *derive.Variable
	Version          *derive.Variable
	Port             *derive.Variable
	ClientID         *derive.Variable
	ClientSecret     *derive.Variable
	RedirectURL      *derive.Variable
	HostedDomain     *derive.Variable
	GroupPrefix      *derive.Variable
	AdminCredentials *derive.Variable
	AdminSubject     *derive.Variable
	// CookieKeySecret, ClientSecretSecret, AdminCredentialsSecret are the secrets by role.
	CookieKeySecret        *derive.Secret
	ClientSecretSecret     *derive.Secret
	AdminCredentialsSecret *derive.Secret
	// The levels by name.
	CoreLevel derive.Level
	DataLevel derive.Level
	SiteLevel derive.Level

	// SecretsFiles names the files declaring the secrets.
	SecretsFiles string
	// SecretExample is the example pin of the first two secrets.
	SecretExample string
	// SecretsCell lists the secrets for the README table.
	SecretsCell string
	// DatabaseVariablesCell lists the database variables for the README table, the
	// common prefix written once.
	DatabaseVariablesCell string
	// DatabaseEnv, DirectoryEnv are the aligned assignment blocks of locals.tf.
	DatabaseEnv  string
	DirectoryEnv string
	// CodeDefaults are the data-level variables left to their code default.
	CodeDefaults []*derive.Variable
	// CodeDefaultsCell lists them for the README.
	CodeDefaultsCell string
	// SiteImageVars names the site-level variables the image sets.
	SiteImageVars string
	// MigrateLevels spells the levels the migration constructs: "core and data".
	MigrateLevels string
	// IdentityLines are the aligned identity lines of the service-accounts header.
	IdentityLines string
	// LabelLines are the extra labels, aligned to the derived ones.
	LabelLines string
	// HostnameDefaults is the aligned default block of var.hostnames.
	HostnameDefaults string
	// HostnamesSummary restates the hostnames in one line: the hosts with the domain
	// written once.
	HostnamesSummary string
	// RestatedDefaults are the aligned lines of the tfvars comment restating defaults.
	RestatedDefaults string
	// BelowProduction spells the hostname shapes below production.
	BelowProduction string
	// HostnamesProse lists the hostnames for the README, wrapped after the first.
	HostnamesProse string
	// IntegrationHost is the integration environment's canonical hostname.
	IntegrationHost string
}

// The width the labels block aligns to: its longest derived key.
const labelsWidth = len("terraform_source_path")

// numberWords spell the small counts the prose uses.
var numberWords = map[int]string{1: "one", 2: "two", 3: "three", 4: "four", 5: "five", 6: "six"}

// newView prepares the view, refusing a model the templates cannot render.
func newView(m *derive.Model) (*view, error) {
	if len(m.Auths) != 1 {
		return nil, errors.Newf("the stack binds one auth; %s has %d", m.App, len(m.Auths))
	}
	p := m.Placement
	v := &view{Model: m, P: p, Prefix: p.Prefix, Integration: p.Integration(), Production: p.Production()}
	v.RepoFullName = repoFullName(m.Repository)
	v.SweepMinute = sweepMinute(m.App)
	v.Auth = &m.Auths[0]
	v.AuthVar = v.Auth.VariablePrefix()
	v.RoutesDir = path.Dir(v.Auth.Callback.File)
	if err := v.roles(); err != nil {
		return nil, err
	}
	v.environments()
	v.regions()
	v.secrets()
	v.blocks()

	return v, nil
}

// roles finds the variables and levels the templates name.
func (v *view) roles() error {
	roles := []struct {
		role derive.Role
		dst  **derive.Variable
	}{
		{derive.RoleServiceName, &v.ServiceName},
		{derive.RoleLoggingProject, &v.LoggingProject},
		{derive.RoleVersion, &v.Version},
		{derive.RolePort, &v.Port},
		{derive.RoleClientID, &v.ClientID},
		{derive.RoleClientSecret, &v.ClientSecret},
		{derive.RoleRedirectURL, &v.RedirectURL},
		{derive.RoleHostedDomain, &v.HostedDomain},
		{derive.RoleGroupPrefix, &v.GroupPrefix},
		{derive.RoleAdminCredentials, &v.AdminCredentials},
		{derive.RoleAdminSubject, &v.AdminSubject},
	}
	for _, r := range roles {
		*r.dst = v.byRole(r.role)
		if *r.dst == nil {
			return errors.Newf("no variable in the %s role: the stack needs one", r.role)
		}
	}
	for _, s := range v.Secrets {
		switch s.Variable.Role {
		case derive.RoleCookieKey:
			v.CookieKeySecret = ptr(s)
		case derive.RoleClientSecret:
			v.ClientSecretSecret = ptr(s)
		case derive.RoleAdminCredentials:
			v.AdminCredentialsSecret = ptr(s)
		default:
		}
	}
	if v.CookieKeySecret == nil || v.ClientSecretSecret == nil || v.AdminCredentialsSecret == nil {
		return errors.New("the stack needs the cookie key, the client secret, and the admin credentials among the secrets")
	}
	levels := []struct {
		name string
		dst  *derive.Level
	}{{derive.LevelCore, &v.CoreLevel}, {derive.LevelData, &v.DataLevel}, {derive.LevelSite, &v.SiteLevel}}
	for _, l := range levels {
		level, ok := v.Level(l.name)
		if !ok {
			return errors.Newf("no %s level", l.name)
		}
		*l.dst = level
	}

	return nil
}

// ptr copies a secret onto the heap.
func ptr(s derive.Secret) *derive.Secret {
	return &s
}

// byRole returns the variable in the role, or nil.
func (v *view) byRole(role derive.Role) *derive.Variable {
	for i := range v.Variables {
		if v.Variables[i].Role == role {
			return &v.Variables[i]
		}
	}

	return nil
}

// environments spells the environments and their hostnames.
func (v *view) environments() {
	envs := v.P.Environments
	quoted := make([]string, 0, len(envs))
	ticked := make([]string, 0, len(envs))
	for _, e := range envs {
		quoted = append(quoted, `"`+e+`"`)
		ticked = append(ticked, "`"+e+"`")
	}
	v.EnvsOr = joinOr(envs)
	v.EnvsComma = strings.Join(envs, ", ")
	v.EnvsBacktickOr = joinOr(ticked)
	v.EnvList = "[" + strings.Join(quoted, ", ") + "]"
	v.order(envs)
	v.EnvCountWord = numberWords[len(envs)]
	if v.EnvCountWord == "" {
		v.EnvCountWord = fmt.Sprint(len(envs))
	}
	v.SharedInstanceEnvs = joinAnd(envs[1:])
	v.SharedInstanceEnvsWrapped = v.SharedInstanceEnvs
	if len(envs) > 2 {
		v.SharedInstanceEnvsWrapped = strings.TrimSuffix(v.SharedInstanceEnvs, " "+v.Production) + "\n  " + v.Production
	}

	strs := make([][2]string, 0, len(envs))
	maps := make([][2]string, 0, len(envs))
	hosts := make([][2]string, 0, len(envs))
	var summary, below []string
	for _, e := range v.Environments {
		strs = append(strs, [2]string{e.Name, `""`})
		maps = append(maps, [2]string{e.Name, "{}"})
		hosts = append(hosts, [2]string{e.Name, `["` + strings.Join(e.Hostnames, `", "`) + `"]`})
		host := e.Hostnames[0]
		if e.Name != v.Production {
			summary = append(summary, strings.TrimSuffix(host, v.P.AppsDomain))
			below = append(below, "app-"+e.Name+".domain")
		} else {
			summary = append(summary, host)
		}
	}
	v.EmptyEnvStrings = aligned("    ", strs)
	v.EmptyEnvMaps = aligned("    ", maps)
	v.TfvarsEnvStrings = aligned("  ", strs)
	v.TfvarsEnvMaps = aligned("  ", maps)
	v.HostnameDefaults = aligned("    ", hosts)
	v.HostnamesSummary = strings.Join(summary, " / ")
	// Named from the one nearest production down.
	slices.Reverse(below)
	v.BelowProduction = joinAnd(below)
	all := make([]string, 0, len(v.Environments))
	for _, e := range v.Environments {
		all = append(all, "`"+e.Hostnames[0]+"`")
	}
	v.HostnamesProse = all[0] + ",\n" + strings.Join(all[1:], ", ")
	v.IntegrationHost = v.Environments[0].Hostnames[0]
}

// regions spells the regions.
func (v *view) regions() {
	codes := make([]string, 0, len(v.P.Regions))
	subs := make([]string, 0, len(v.P.Regions))
	for _, r := range v.P.Regions {
		codes = append(codes, r.Code)
		subs = append(subs, r.Name+":"+r.Code)
	}
	v.RegionCodes = strings.Join(codes, "|")
	v.RegionsSub = strings.Join(subs, ",")
	v.PrimaryCode = v.P.Primary().Code
}

// secrets spells the secrets.
func (v *view) secrets() {
	var files, example []string
	cells := make([]string, 0, len(v.Secrets))
	for i, s := range v.Secrets {
		if len(files) == 0 || files[len(files)-1] != s.Variable.File {
			files = append(files, s.Variable.File)
		}
		cells = append(cells, "`"+s.Variable.Name+"`")
		if i < 2 {
			example = append(example, s.Variable.Name+` = "1"`)
		}
	}
	v.SecretsFiles = strings.Join(files, ", ")
	v.SecretsCell = strings.Join(cells, ", ")
	v.SecretExample = strings.Join(example, ", ")
}

// blocks prepares the aligned blocks and the remaining phrases.
func (v *view) blocks() {
	db := v.Database
	v.DatabaseEnv = aligned("    ", [][2]string{
		{db.Project.Name, "local.instance.project"},
		{db.Instance.Name, "local.instance.name"},
		{db.Name.Name, "local.database_name"},
	})
	v.DirectoryEnv = aligned("    ", [][2]string{
		{v.HostedDomain.Name, "var." + v.AuthVar + "_hosted_domain"},
		{v.GroupPrefix.Name, "var." + v.AuthVar + "_group_prefix"},
	})
	prefix := commonPrefix(db.Project.Name, db.Instance.Name, db.Name.Name)
	v.DatabaseVariablesCell = "`" + db.Project.Name + "`, `" + strings.TrimPrefix(db.Instance.Name, prefix) + "`, `" + strings.TrimPrefix(db.Name.Name, prefix) + "`"

	var defaults []string
	for _, x := range v.ByLevel(v.DataLevel.Name) {
		if x.Supply() == derive.SupplyDefault {
			v.CodeDefaults = append(v.CodeDefaults, x)
			defaults = append(defaults, "`"+x.Name+"` (code default)")
		}
	}
	v.CodeDefaultsCell = strings.Join(defaults, ", ")
	var image []string
	for _, x := range v.ByLevel(v.SiteLevel.Name) {
		if x.Image {
			image = append(image, x.Name)
		}
	}
	v.SiteImageVars = joinAnd(image)
	v.MigrateLevels = joinAnd(v.Migrate.Levels)

	stem := v.Prefix + "-<env>-gbl-" + v.App + "-"
	v.IdentityLines = aligned("#   ", [][2]string{
		{stem + v.Site.Name, v.Site.Main + ", the served site (Cloud Run service)"},
		{stem + v.Migrate.Name, v.Migrate.Dir + " (Cloud Run job)"},
	}, "  ")

	keys := make([]string, 0, len(v.P.Labels))
	width := labelsWidth
	for k := range v.P.Labels {
		keys = append(keys, k)
		width = max(width, len(k))
	}
	sort.Strings(keys)
	labels := make([]string, 0, len(keys))
	for _, k := range keys {
		labels = append(labels, fmt.Sprintf("    %-*s = %q", width, k, v.P.Labels[k]))
	}
	v.LabelLines = strings.Join(labels, "\n")

	v.RestatedDefaults = aligned("#   ", [][2]string{
		{"hostnames", v.HostnamesSummary},
		{"placeholder_image", v.P.PlaceholderImage},
		{v.AuthVar + "_group_prefix", `"` + v.Auth.GroupPrefixDefault + `"`},
		{v.AuthVar + "_hosted_domain", `"` + v.P.HostedDomain + `"`},
	}, " = ")
}

// Purpose says what a secret is for: the framework's words for the roles it knows,
// the field's doc comment for a secret of the application's own.
func (v *view) Purpose(s *derive.Secret) string {
	switch s.Variable.Role {
	case derive.RoleCookieKey:
		return "Signs session cookies and seals list cursors: Base64 of 32+ random bytes. Rotating it ends every session and cursor."
	case derive.RoleClientSecret:
		return "OAuth client secret of the application's registration with Google (pairs with " + v.ClientID.Name + ")."
	case derive.RoleAdminCredentials:
		return "Service-account key (JSON) with domain-wide delegation for the Admin SDK groups scope, through which the " + v.Auth.Name + " auth reads role groups."
	default:
		if s.Variable.Doc != "" {
			return firstSentence(s.Variable.Doc)
		}

		return "A credential " + s.Variable.Declaration() + " reads."
	}
}

// aligned writes key = value lines with the values in one column. The separator
// defaults to " = ".
func aligned(indent string, pairs [][2]string, separator ...string) string {
	sep := " = "
	if len(separator) > 0 {
		sep = separator[0]
	}
	width := 0
	for _, p := range pairs {
		width = max(width, len(p[0]))
	}
	lines := make([]string, 0, len(pairs))
	for _, p := range pairs {
		lines = append(lines, indent+fmt.Sprintf("%-*s", width, p[0])+sep+p[1])
	}

	return strings.Join(lines, "\n")
}

// joinOr writes "a, b, or c".
func joinOr(items []string) string {
	return joinWith(items, "or")
}

// joinAnd writes "a, b, and c" (or "a and b").
func joinAnd(items []string) string {
	return joinWith(items, "and")
}

func joinWith(items []string, word string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " " + word + " " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + ", " + word + " " + items[len(items)-1]
	}
}

// commonPrefix is the longest prefix the names share, cut at an underscore.
func commonPrefix(names ...string) string {
	if len(names) == 0 {
		return ""
	}
	prefix := names[0]
	for _, n := range names[1:] {
		for !strings.HasPrefix(n, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	if i := strings.LastIndex(prefix, "_"); i >= 0 {
		prefix = prefix[:i]
	}

	return prefix
}

// firstSentence returns the doc comment's first sentence, on one line.
func firstSentence(doc string) string {
	text := strings.Join(strings.Fields(doc), " ")
	if i := strings.Index(text, ". "); i >= 0 {
		return text[:i+1]
	}

	return text
}

// order fills the promotion-order fields: which environments wait for approval, and
// the environment before each.
func (v *view) order(envs []string) {
	approvals := make([]string, 0, len(v.P.ApprovalEnvironments()))
	for _, env := range v.P.ApprovalEnvironments() {
		approvals = append(approvals, strconv.Quote(env))
	}
	v.ApprovalList = "[" + strings.Join(approvals, ", ") + "]"
	v.ApprovalsProse = joinAnd(v.P.ApprovalEnvironments())
	if v.ApprovalsProse == "" {
		v.ApprovalsProse = "no environment"
	}
	previous := make([]string, 0, len(envs))
	for _, env := range envs {
		previous = append(previous, env+" = "+strconv.Quote(v.P.Previous(env)))
	}
	v.PreviousEnvMap = "{ " + strings.Join(previous, ", ") + " }"
}

// repoFullName is owner/name from a GitHub repository URL, or the placeholder the
// pipeline reads as a refusal.
func repoFullName(repository string) string {
	const host = "https://github.com/"
	if !strings.HasPrefix(repository, host) {
		return "REPOSITORY_NOT_ON_GITHUB"
	}

	return strings.TrimSuffix(strings.TrimPrefix(repository, host), "/")
}

// sweepMinute spreads the applications' hourly sweeps over the hour: the minute is a
// hash of the application code, stable across renders.
func sweepMinute(app string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(app))

	return int(h.Sum32() % 60)
}
