package app

import "strconv"

// optionKind is the option interface an option constructor returns.
type optionKind int

const (
	kindResourceOption optionKind = iota + 1
	kindTSOption
	kindOutletOption
)

func (k optionKind) String() string {
	switch k {
	case kindResourceOption:
		return "ResourceOption"
	case kindTSOption:
		return "TSOption"
	case kindOutletOption:
		return "OutletOption"
	default:
		return "option"
	}
}

// paramKind is the kind of value an option parameter accepts.
type paramKind int

const (
	paramNone paramKind = iota
	paramAny
	paramString
	paramBool
	paramStringMap
	paramBoolMap
	paramComposite
	paramTSOption
	paramOutletOption
	// paramAuthMethod is a sign-in method declaration: a call to one of the generation
	// package's method constructors (generation.PasswordMethod(),
	// generation.GoogleMethod("directory")), with its label when one is chained
	// (.Labeled("Company directory")) and the registration's flags when they are
	// (.InvitedUsers(), .SyncsRoles(), .OpenSignUp(), .UsernameIsEmail()).
	paramAuthMethod
	// paramRelease is a release: a semantic version string literal, or the generation
	// package's ThisRelease identifier for the server's own release.
	paramRelease
	// paramInt is a whole number: an integer literal, or a constant expression of integer
	// literals (2<<20).
	paramInt
	// paramRequestLog is a request log word: a call to one of the generation package's
	// RequestLog constructors (generation.LogOnEvent()), with its MinSeverity floor when
	// one is chained.
	paramRequestLog
	// paramTraces is a trace setting: a call to one of the generation package's Traces
	// constructors (generation.TracesOff()).
	paramTraces
)

// ThisReleaseIdent is the generation package's identifier naming the server's own
// release as an outlet's oldest answered release (generation.ThisRelease).
const ThisReleaseIdent = "ThisRelease"

func (k paramKind) String() string {
	switch k {
	case paramString:
		return "string literal"
	case paramBool:
		return "bool literal"
	case paramStringMap:
		return "map[string]string literal"
	case paramBoolMap:
		return "map[string]bool literal"
	case paramComposite:
		return "composite literal"
	case paramTSOption:
		return "TSOption"
	case paramOutletOption:
		return "OutletOption"
	case paramAuthMethod:
		return "sign-in method declaration (generation.PasswordMethod(), GoogleMethod(name), AzureMethod(name), or WorkOSMethod(name), with .Labeled(label), .InvitedUsers(), .SyncsRoles(), .OpenSignUp() or .UsernameIsEmail() when chained)"
	case paramRelease:
		return "release string literal (\"1.5.0\") or generation." + ThisReleaseIdent
	case paramInt:
		return "whole number literal (4194304) or constant expression (2<<20)"
	case paramRequestLog:
		return "generation.<RequestLog> call (LogAlways(), LogOnEvent(), LogSampled(fraction) or LogNever(), with .MinSeverity(logging.<Severity>) chained when wanted)"
	case paramTraces:
		return "generation.<Traces> call (TracesFollowFrontEnd(), TracesCapped(rate) or TracesOff())"
	case paramAny, paramNone:
		return "argument"
	default:
		return "argument"
	}
}

func (k paramKind) argKind() ArgKind {
	switch k {
	case paramString:
		return ArgString
	case paramBool:
		return ArgBool
	case paramStringMap:
		return ArgStringMap
	case paramBoolMap:
		return ArgBoolMap
	case paramComposite:
		return ArgComposite
	case paramTSOption, paramOutletOption:
		return ArgCall
	case paramAuthMethod:
		return ArgAuthMethod
	case paramRelease:
		return ArgString
	case paramInt:
		return ArgInt
	case paramRequestLog, paramTraces:
		return ArgConstructor
	case paramAny, paramNone:
		return ArgOther
	default:
		return ArgOther
	}
}

func (k paramKind) optionKind() optionKind {
	switch k {
	case paramTSOption:
		return kindTSOption
	case paramOutletOption:
		return kindOutletOption
	case paramNone, paramAny, paramString, paramBool, paramStringMap, paramBoolMap, paramComposite, paramAuthMethod, paramRelease, paramInt, paramRequestLog, paramTraces:
		return 0
	default:
		return 0
	}
}

// Option constructor names the profile reads beyond the positional accessors.
const (
	optServesSessions     = "ServesSessions"
	optGenerateHandlers   = "GenerateHandlers"
	optWithImports        = "WithImports"
	optGenerateRoutes     = "GenerateRoutes"
	optGenerateRouter     = "GenerateRouter"
	optGenerateTypescript = "GenerateTypescript"
	optAuth               = "Auth"
	optAPIKey             = "APIKey"
	optTokens             = "Tokens"
	optWebApp             = "WebApp"
	optManages            = "Manages"
	optOldestAnswered     = "OldestAnswered"
	optWithRequestLog     = "WithRequestLog"
	optWithMountedRoutes  = "WithMountedRoutes"
	optWithRouterOutlet   = "WithRouterOutlet"
	optOutletRequestLog   = "OutletRequestLog"
	optOutletTraces       = "OutletTraces"
)

// The generation package's sign-in method constructors, as a program writes them
// (generation.GoogleMethod("directory")), by the method each declares. The password
// method takes no name, since it is always registered as password; the others take the
// registration name. They mirror github.com/cccteam/ccc/resource/generation (options.go).
var authMethodConstructors = map[string]string{
	"PasswordMethod": MethodPassword,
	"GoogleMethod":   MethodGoogle,
	"AzureMethod":    MethodAzure,
	"WorkOSMethod":   MethodWorkOS,
}

// labeledMethod is the method that chains a label onto a declaration
// (generation.GoogleMethod("directory").Labeled("Company directory"));
// invitedUsersMethod, syncsRolesMethod, openSignUpMethod and usernameIsEmailMethod chain
// the registration's flags: the directory admits only users an operator created, the
// directory's groups write the user's roles at every sign-in, the method admits anyone
// (open sign-up: the rule a self-service tenant record needs of a directory, and on the
// password method a sign-up from a mailed link), and the password method's sign-in name
// is the person's email address. They mirror generation.AuthMethod's methods.
const (
	labeledMethod         = "Labeled"
	invitedUsersMethod    = "InvitedUsers"
	syncsRolesMethod      = "SyncsRoles"
	openSignUpMethod      = "OpenSignUp"
	usernameIsEmailMethod = "UsernameIsEmail"
)

// MethodConstructor returns the generation package's constructor for a sign-in method
// (PasswordMethod for password, GoogleMethod for google), or empty for a method the
// generated router does not compose.
func MethodConstructor(kind string) string {
	for ident, k := range authMethodConstructors {
		if k == kind {
			return ident
		}
	}

	return ""
}

// MethodDeclaration writes a new sign-in method's declaration as a generator program
// carries it, package-qualified: generation.PasswordMethod(), or
// generation.GoogleMethod("directory"), with .Labeled(label) when the label is set. A
// declaration read from a program is written back by AuthMethodArg.Declaration, which
// keeps the flags chained on it.
func MethodDeclaration(kind, name, label string) string {
	return (&AuthMethodArg{Kind: kind, Name: name, Label: label}).Declaration()
}

// Declaration writes the declaration as a generator program carries it, package-qualified:
// the constructor with its registration name, then .Labeled(label), .InvitedUsers(),
// .SyncsRoles(), .OpenSignUp() and .UsernameIsEmail() for what is set. Empty for a method
// the generated router does not compose.
func (m *AuthMethodArg) Declaration() string {
	ctor := MethodConstructor(m.Kind)
	if ctor == "" {
		return ""
	}
	text := "generation." + ctor + "("
	if m.Kind != MethodPassword {
		text += strconv.Quote(m.Name)
	}
	text += ")"
	if m.Label != "" {
		text += "." + labeledMethod + "(" + strconv.Quote(m.Label) + ")"
	}
	if m.InvitedUsers {
		text += "." + invitedUsersMethod + "()"
	}
	if m.SyncsRoles {
		text += "." + syncsRolesMethod + "()"
	}
	if m.OpenSignUp {
		text += "." + openSignUpMethod + "()"
	}
	if m.UsernameIsEmail {
		text += "." + usernameIsEmailMethod + "()"
	}

	return text
}

// constructorSpec describes one constructor of a declared value, a request log word or a
// trace setting: the name of the number it takes, empty when it takes none.
type constructorSpec struct {
	number string
}

// requestLogConstructors are the generation package's RequestLog constructors, as a
// program writes them (generation.LogSampled(0.1)): the words WithRequestLog,
// OutletRequestLog and WithMountedRoutes take. They mirror
// github.com/cccteam/ccc/resource/generation (requestlog.go), as knownOptions mirrors its
// options.
var requestLogConstructors = map[string]constructorSpec{
	"LogAlways":  {},
	"LogOnEvent": {},
	"LogSampled": {number: "fraction"},
	"LogNever":   {},
}

// tracesConstructors are the generation package's Traces constructors: the settings
// OutletTraces and WithMountedRoutes take.
var tracesConstructors = map[string]constructorSpec{
	"TracesFollowFrontEnd": {},
	"TracesCapped":         {number: "rate"},
	"TracesOff":            {},
}

// minSeverityMethod is the RequestLog method that chains a severity floor onto a word
// (generation.LogOnEvent().MinSeverity(logging.Warning)). A trace setting chains nothing.
const minSeverityMethod = "MinSeverity"

// loggingImportPath is the import path of the logging library whose severity constants
// a floor names; loggingPackageName is the qualifier a recorded floor is written with,
// whatever local name a program imports the library under.
const (
	loggingImportPath  = "cloud.google.com/go/logging"
	loggingPackageName = "logging"
)

// severityIdents are the logging library's severity constants, as a program writes them
// (logging.Warning), lowest first; severityList names them in a finding.
var severityIdents = []string{"Default", "Debug", "Info", "Notice", "Warning", "Error", "Critical", "Alert", "Emergency"}

const severityList = "Default, Debug, Info, Notice, Warning, Error, Critical, Alert or Emergency"

// retiredOptions are the generation option constructors an earlier release knew and the
// framework has since removed, each with where its declaration went. A program still
// using one fails the generator-program check naming the replacement, since the
// framework no longer compiles it.
var retiredOptions = map[string]string{
	"WithDomainRoute": "the tenant segment derives from the tenant record, the resource struct annotated @tenant; remove the option and annotate the record",
}

// optionSpec describes one known option constructor: what it returns and what it takes.
type optionSpec struct {
	kind     optionKind
	params   []paramKind
	variadic paramKind
}

// knownOptions is the set of generation option constructors this impulse release knows,
// keyed by name. It mirrors the exported constructors of
// github.com/cccteam/ccc/resource/generation (options.go). A generator program using a
// constructor missing here fails the generator-program check: the release must learn the
// option before the framework grows it, which is what keeps the two in step.
var knownOptions = map[string]optionSpec{
	optGenerateHandlers:          {kind: kindResourceOption, params: []paramKind{paramString}},
	"GenerateHandlerTests":       {kind: kindResourceOption, params: []paramKind{paramString}},
	"ApplicationName":            {kind: kindResourceOption, params: []paramKind{paramString}},
	optGenerateRoutes:            {kind: kindResourceOption, params: []paramKind{paramString, paramString}, variadic: paramOutletOption},
	optGenerateRouter:            {kind: kindResourceOption},
	"WithRouterOutlet":           {kind: kindResourceOption, params: []paramKind{paramString, paramString}, variadic: paramOutletOption},
	"WithConcealedDomains":       {kind: kindResourceOption},
	optGenerateTypescript:        {kind: kindResourceOption, params: []paramKind{paramString}, variadic: paramTSOption},
	"WithManualResources":        {kind: kindResourceOption, variadic: paramComposite},
	"WithSpannerEmulatorVersion": {kind: kindResourceOption, params: []paramKind{paramString}},
	"WithPluralOverrides":        {kind: kindResourceOption, params: []paramKind{paramStringMap}},
	"CaserInitialismOverrides":   {kind: kindResourceOption, params: []paramKind{paramBoolMap}},
	"WithConsolidatedHandlers":   {kind: kindResourceOption, params: []paramKind{paramString, paramBool}, variadic: paramString},
	"WithVirtualResources":       {kind: kindResourceOption, params: []paramKind{paramString}},
	"WithComputedResources":      {kind: kindResourceOption, params: []paramKind{paramString}},
	"WithRPC":                    {kind: kindResourceOption, params: []paramKind{paramString}},
	"WithTypes":                  {kind: kindResourceOption, params: []paramKind{paramString}},
	optWithImports:               {kind: kindResourceOption, variadic: paramString},
	"WithBodyLimit":              {kind: kindResourceOption, params: []paramKind{paramInt}},
	optWithRequestLog:            {kind: kindResourceOption, params: []paramKind{paramRequestLog}},
	optWithMountedRoutes:         {kind: kindResourceOption, params: []paramKind{paramString, paramRequestLog, paramTraces}},

	optServesSessions:   {kind: kindOutletOption},
	optAuth:             {kind: kindOutletOption, params: []paramKind{paramString}, variadic: paramAuthMethod},
	optAPIKey:           {kind: kindOutletOption},
	optTokens:           {kind: kindOutletOption, params: []paramKind{paramString}},
	optWebApp:           {kind: kindOutletOption, params: []paramKind{paramString}},
	optManages:          {kind: kindOutletOption, params: []paramKind{paramString}},
	optOldestAnswered:   {kind: kindOutletOption, params: []paramKind{paramRelease}},
	optOutletRequestLog: {kind: kindOutletOption, params: []paramKind{paramRequestLog}},
	optOutletTraces:     {kind: kindOutletOption, params: []paramKind{paramTraces}},

	"ForOutlet":           {kind: kindTSOption, params: []paramKind{paramString}},
	"GeneratePermissions": {kind: kindTSOption},
	"GenerateMetadata":    {kind: kindTSOption},
	"GenerateEnums":       {kind: kindTSOption},
}
