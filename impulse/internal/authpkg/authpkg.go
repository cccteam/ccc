// Package authpkg renders an auth package: pkg/auth/<name>, the package that owns one
// session Auth (its tables, its cookies, its permission store and its role file) and the
// sign-in methods registered on it, one file per method. The skeleton candidates carry the
// rendering for their auths, and the transitions that add an auth or a method to an
// application render from the same source, so the two never drift: a test holds each
// candidate's auth packages to what Render produces for them.
//
// A method's settings come from the environment under one convention, which bedrock reads
// too: APP_<AUTH>_<REGISTRATION>_<FIELD>, the auth's name and the registration's name
// upper-cased with every hyphen an underscore (APP_MEMBERS_DIRECTORY_CLIENT_ID). The
// fields a method takes depend on its kind, its organization slot and its authority (see
// Method.Fields).
package authpkg

import (
	"bytes"
	"go/format"
	"regexp"
	"sort"
	"strings"

	// The templates render Go source files, never HTML, so text/template is the right package.
	"text/template" // nosemgrep: go.lang.security.audit.xss.import-text-template.import-text-template

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/names"
)

// Spec describes the auth package to render.
type Spec struct {
	// Module is the application's module path, which the package's imports of the
	// application's own packages start with.
	Module string
	// AuthDir is the directory the auth packages live under, root-relative (pkg/auth).
	AuthDir string
	// Name is the auth's name: the package, the cookie, the table prefix in PascalCase.
	Name string
	// Methods are the sign-in methods registered on the Auth, in order; the first is
	// what the login page offers first.
	Methods []Method
	// Doc is a sentence for the package comment saying who the population is ("the
	// people who sign in to the console"); empty for a generic one.
	Doc string
}

// Method is one sign-in method of the auth.
type Method struct {
	// Kind is the method: app.MethodPassword, app.MethodAzure or app.MethodGoogle.
	Kind string
	// Name is the registration name (user/<name>/login); password for the password
	// method, which takes no other.
	Name string
	// Authority is who owns role membership for a directory method: app.AuthorityDirectory
	// (the registration synchronizes roles from the directory at every sign-in) or
	// app.AuthorityApplication (roles are assigned in the application). Ignored for the
	// password method.
	Authority string
	// Slot is the organization slot of a directory method: which organizations' accounts
	// the registration admits. Azure: SlotOne (one tenant), SlotListed (named tenants),
	// SlotAny (any tenant), SlotPersonal (Microsoft personal accounts alone),
	// SlotOnePersonal (one tenant and personal accounts), SlotAnyPersonal (any tenant and
	// personal accounts). Google: SlotOne (one Workspace), SlotListed (named Workspaces),
	// SlotAnyWorkspace (any Workspace), SlotAny (any Google account), SlotMixed (one
	// Workspace and any Google account). Empty is SlotOne.
	Slot string
	// Accounts is the standard account rule for a first sign-in: AccountsMembers (members
	// of the directory, the default), AccountsInvited (invited users only) or
	// AccountsOpen (open sign-up). Empty is AccountsMembers.
	Accounts string
}

// The organization slots.
const (
	SlotOne          = "one"
	SlotListed       = "listed"
	SlotAny          = "any"
	SlotPersonal     = "personal"
	SlotOnePersonal  = "one-personal"
	SlotAnyPersonal  = "any-personal"
	SlotAnyWorkspace = "any-workspace"
	SlotMixed        = "mixed"
)

// The standard account rules.
const (
	AccountsMembers = "members"
	AccountsInvited = "invited"
	AccountsOpen    = "open"
)

// Slots lists the slots a method kind takes, in the order the help shows them.
func Slots(kind string) []string {
	switch kind {
	case app.MethodAzure:
		return []string{SlotOne, SlotListed, SlotAny, SlotPersonal, SlotOnePersonal, SlotAnyPersonal}
	case app.MethodGoogle:
		return []string{SlotOne, SlotListed, SlotAnyWorkspace, SlotAny, SlotMixed}
	default:
		return nil
	}
}

// Accounts lists the standard account rules.
func Accounts() []string { return []string{AccountsMembers, AccountsInvited, AccountsOpen} }

// Validate checks a method against the library's rules, as the session library enforces
// them at registration: a slot the kind has, an account rule the slot allows (open sign-up
// only with any organization or personal accounts; members of the directory not with
// them), and role synchronization only with one organization.
func (m *Method) Validate() error {
	if m.Kind == app.MethodPassword {
		if m.Name != "" && m.Name != app.PasswordRegistration {
			return errors.Newf("the password method is always registered as %q", app.PasswordRegistration)
		}
		if m.Slot != "" || m.Accounts != "" || m.Authority != "" {
			return errors.New("the password method takes no slot, account rule or authority")
		}

		return nil
	}
	if m.Kind != app.MethodAzure && m.Kind != app.MethodGoogle {
		return errors.Newf("%q is not a sign-in method impulse composes: password, azure or google", m.Kind)
	}
	if !registrationRE.MatchString(m.Name) {
		return errors.Newf("registration %q: lowercase letters, digits and hyphens, starting with a letter (directory, entra-one)", m.Name)
	}
	if m.Name == app.PasswordRegistration {
		return errors.Newf("%q is the password method's registration name", m.Name)
	}
	slot := m.slot()
	known := false
	for _, s := range Slots(m.Kind) {
		if s == slot {
			known = true
		}
	}
	if !known {
		return errors.Newf("slot %q: a %s method takes %s", m.Slot, m.Kind, strings.Join(Slots(m.Kind), ", "))
	}
	accounts := m.accounts()
	if accounts != AccountsMembers && accounts != AccountsInvited && accounts != AccountsOpen {
		return errors.Newf("accounts %q: members, invited or open", m.Accounts)
	}
	open := m.openSlot()
	switch {
	case accounts == AccountsOpen && !open:
		return errors.Newf("open sign-up is for any organization or personal accounts; a registration naming its %s takes members or invited", m.organizations())
	case accounts == AccountsMembers && open:
		return errors.Newf("members of the directory with any organization or personal accounts would sign up the world; take invited or open")
	}
	switch m.Authority {
	case app.AuthorityDirectory:
		if slot != SlotOne {
			return errors.Newf("role synchronization follows one %s's roles; a registration admitting several takes the application authority", m.organization())
		}
	case app.AuthorityApplication:
	default:
		return errors.Newf("authority %q: directory or application", m.Authority)
	}

	return nil
}

// registrationRE is the shape of a registration name, as the session library takes it.
var registrationRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func (m *Method) slot() string {
	if m.Slot == "" {
		return SlotOne
	}

	return m.Slot
}

func (m *Method) accounts() string {
	if m.Accounts == "" {
		return AccountsMembers
	}

	return m.Accounts
}

// openSlot reports a slot that admits the world: any organization or personal accounts.
func (m *Method) openSlot() bool {
	switch m.slot() {
	case SlotAny, SlotPersonal, SlotOnePersonal, SlotAnyPersonal, SlotAnyWorkspace, SlotMixed:
		return true
	default:
		return false
	}
}

// organization is the word for the method's organization: tenant or Workspace.
func (m *Method) organization() string {
	if m.Kind == app.MethodGoogle {
		return "Workspace"
	}

	return "tenant"
}

func (m *Method) organizations() string { return m.organization() + "s" }

// Pascal is the registration name in PascalCase: the handler and settings prefix
// (DirectoryLogin, DirectorySettings; entra-one is EntraOne).
func (m *Method) Pascal() string {
	var b strings.Builder
	for _, part := range strings.Split(m.Name, "-") {
		b.WriteString(names.Pascal(part))
	}

	return b.String()
}

// Upper is the registration name as a variable segment: upper-cased, hyphens as
// underscores (ENTRA_ONE).
func (m *Method) Upper() string {
	return strings.ToUpper(strings.ReplaceAll(m.Name, "-", "_"))
}

// Directory reports a method whose people are sent to a directory or identity provider.
func (m *Method) Directory() bool { return m.Kind != app.MethodPassword }

// Field is one setting a method reads from the environment.
type Field struct {
	// Name is the field on the method's settings struct (ClientID).
	Name string
	// Suffix is the variable's last segment (CLIENT_ID).
	Suffix string
	// Secret marks a credential the environment keeps as a secret.
	Secret bool
	// Doc describes the field.
	Doc string
	// Dev is what the development environment template sets, empty for a commented
	// placeholder a person fills in.
	Dev string
	// Simulated reports a field the simulated directory (skipAuth) reads too, so it is
	// set in development.
	Simulated bool
}

// The names of a method's settings fields, as the rendered settings struct spells them
// and as the development template and the data level edits refer to them.
const (
	FieldTenantID     = "TenantID"
	FieldTenants      = "Tenants"
	FieldClientID     = "ClientID"
	FieldClientSecret = "ClientSecret"
	FieldRedirectURL  = "RedirectURL"
	FieldHostedDomain = "HostedDomain"
	FieldWorkspaces   = "Workspaces"
	FieldGroupPrefix  = "GroupPrefix"
	FieldGroupLookup  = "GroupLookup"
)

// Fields lists the settings a method reads, in order: what the method's settings struct
// holds, what the data level reads from the environment and what bedrock supplies per
// environment.
func (m *Method) Fields() []Field {
	if !m.Directory() {
		return nil
	}
	var fields []Field
	switch m.Kind {
	case app.MethodAzure:
		switch m.slot() {
		case SlotOne, SlotOnePersonal:
			fields = append(fields, Field{Name: FieldTenantID, Suffix: "TENANT_ID", Simulated: true, Doc: "the tenant whose accounts the registration admits, by its tenant id (the tid claim, a GUID); the slot is validated at registration, so the simulated directory needs a well-formed placeholder"})
		case SlotListed:
			fields = append(fields, Field{Name: FieldTenants, Suffix: "TENANTS", Simulated: true, Doc: "the tenants the registration admits, each named: <name>=<tenant id>, comma-separated (lab-a=<guid>,lab-b=<guid>); the name is the tenant key every link made through it carries, and the slot is validated at registration, so the simulated directory needs well-formed placeholders"})
		}
		fields = append(fields,
			Field{Name: FieldClientID, Suffix: "CLIENT_ID", Doc: "the app registration's application (client) id"},
			Field{Name: FieldClientSecret, Suffix: "CLIENT_SECRET", Secret: true, Doc: "the app registration's client secret"},
			Field{Name: FieldRedirectURL, Suffix: "REDIRECT_URL", Simulated: true, Doc: "the browser-facing callback the directory returns the browser to: <base>/<outlet>/api/user/" + m.Name + "/callback"},
		)
	case app.MethodGoogle:
		switch m.slot() {
		case SlotOne, SlotMixed:
			fields = append(fields, Field{Name: FieldHostedDomain, Suffix: "HOSTED_DOMAIN", Simulated: true, Doc: "the Workspace's domain, checked against the verified hd claim"})
		case SlotListed:
			fields = append(fields, Field{Name: FieldWorkspaces, Suffix: "WORKSPACES", Simulated: true, Doc: "the Workspaces the registration admits, each named with its domains: <name>=<domain>[+<domain>], comma-separated (lab=example.com,second=other.org); the name is the connection every link made through it carries"})
		}
		fields = append(fields,
			Field{Name: FieldClientID, Suffix: "CLIENT_ID", Doc: "the OAuth client's id"},
			Field{Name: FieldClientSecret, Suffix: "CLIENT_SECRET", Secret: true, Doc: "the OAuth client's secret"},
			Field{Name: FieldRedirectURL, Suffix: "REDIRECT_URL", Simulated: true, Doc: "the browser-facing callback Google returns the browser to: <base>/<outlet>/api/user/" + m.Name + "/callback"},
		)
		if m.Authority == app.AuthorityDirectory {
			fields = append(fields,
				Field{Name: FieldGroupPrefix, Suffix: "GROUP_PREFIX", Simulated: true, Doc: "the local-part prefix of the Google Groups that carry roles: <prefix><role>@<domain> assigns <role>"},
				Field{Name: FieldGroupLookup, Suffix: "GROUP_LOOKUP", Doc: "how far the groups lookup reaches: direct (the groups the person is a direct member of) or nested (the groups above those too, level by level); empty is direct"},
			)
		}
	}

	return fields
}

// Variable is the environment variable of a field under the convention:
// APP_<AUTH>_<REGISTRATION>_<FIELD>.
func (m *Method) Variable(auth string, f *Field) string {
	return "APP_" + strings.ToUpper(auth) + "_" + m.Upper() + "_" + f.Suffix
}

// SettingsType is the name of the method's settings struct (DirectorySettings).
func (m *Method) SettingsType() string { return m.Pascal() + "Settings" }

// Registration is the exported constant naming the registration (DirectoryRegistration).
func (m *Method) Registration() string { return m.Pascal() + "Registration" }

// Provider names who the browser is sent to.
func (m *Method) Provider() string {
	switch m.Kind {
	case app.MethodAzure:
		return "Microsoft"
	case app.MethodGoogle:
		return "Google"
	default:
		return ""
	}
}

// Files renders the auth package: the file name (relative to the package directory)
// and its content, formatted. One file for the Auth and one per method.
func Files(spec *Spec) (map[string][]byte, error) {
	if spec.Name == "" || spec.Module == "" || spec.AuthDir == "" {
		return nil, errors.New("authpkg.Spec needs Module, AuthDir and Name")
	}
	if len(spec.Methods) == 0 {
		return nil, errors.Newf("the %s auth needs at least one sign-in method", spec.Name)
	}
	seen := map[string]bool{}
	for i := range spec.Methods {
		m := &spec.Methods[i]
		if m.Kind == app.MethodPassword {
			m.Name = app.PasswordRegistration
		}
		if err := m.Validate(); err != nil {
			return nil, errors.Wrapf(err, "the %s auth's %s method", spec.Name, m.Name)
		}
		if seen[m.Name] {
			return nil, errors.Newf("the %s auth registers %s twice", spec.Name, m.Name)
		}
		seen[m.Name] = true
	}
	files := map[string][]byte{}
	main, err := render("auth", authTemplate, newView(spec))
	if err != nil {
		return nil, err
	}
	files[spec.Name+".go"] = main
	for i := range spec.Methods {
		m := &spec.Methods[i]
		var tmpl string
		switch m.Kind {
		case app.MethodPassword:
			tmpl = loginFormTemplate
		case app.MethodAzure:
			tmpl = azureTemplate
		case app.MethodGoogle:
			tmpl = googleTemplate
		}
		out, err := render(m.Name, tmpl, newMethodView(spec, m))
		if err != nil {
			return nil, err
		}
		files[m.Name+".go"] = out
	}

	return files, nil
}

// MethodFile renders one method's file of the auth package, for a method added to an
// auth that exists: the method's registration constant, settings, handlers and
// registration function.
func MethodFile(spec *Spec, m *Method) ([]byte, error) {
	if m.Kind == app.MethodPassword {
		m.Name = app.PasswordRegistration
	}
	if err := m.Validate(); err != nil {
		return nil, errors.Wrapf(err, "the %s auth's %s method", spec.Name, m.Name)
	}
	var tmpl string
	switch m.Kind {
	case app.MethodPassword:
		tmpl = loginFormTemplate
	case app.MethodAzure:
		tmpl = azureTemplate
	case app.MethodGoogle:
		tmpl = googleTemplate
	}

	return render(m.Name, tmpl, newMethodView(spec, m))
}

// FileNames lists the files Files renders, sorted.
func FileNames(spec *Spec) []string {
	files := make([]string, 0, 1+len(spec.Methods))
	files = append(files, spec.Name+".go")
	for i := range spec.Methods {
		name := spec.Methods[i].Name
		if spec.Methods[i].Kind == app.MethodPassword {
			name = app.PasswordRegistration
		}
		files = append(files, name+".go")
	}
	sort.Strings(files)

	return files
}

func render(name, text string, data any) ([]byte, error) {
	t, err := template.New(name).Funcs(template.FuncMap{"join": strings.Join}).Parse(text)
	if err != nil {
		return nil, errors.Wrapf(err, "template.Parse(): %s", name)
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return nil, errors.Wrapf(err, "template.Execute(): %s", name)
	}
	out, err := format.Source(b.Bytes())
	if err != nil {
		return nil, errors.Wrapf(err, "format.Source(): the rendered %s file", name)
	}

	return out, nil
}
