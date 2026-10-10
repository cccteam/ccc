package app

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
	// paramFlavor is an auth flavor: one of the generation package's AuthFlavor
	// constants, written as generation.<Flavor>.
	paramFlavor
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
	case paramFlavor:
		return "generation.<Flavor> identifier (Password, OIDCGoogle, or OIDCAzure)"
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
	case paramFlavor:
		return ArgIdent
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
	case paramNone, paramAny, paramString, paramBool, paramStringMap, paramBoolMap, paramComposite, paramFlavor, paramRelease, paramInt, paramRequestLog, paramTraces:
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
	optWebApp             = "WebApp"
	optOldestAnswered     = "OldestAnswered"
	optWithRequestLog     = "WithRequestLog"
	optWithMountedRoutes  = "WithMountedRoutes"
	optOutletRequestLog   = "OutletRequestLog"
	optOutletTraces       = "OutletTraces"
)

// The generation package's AuthFlavor identifiers, as a program writes them
// (generation.Password).
const (
	FlavorIdentPassword   = "Password"
	FlavorIdentOIDCGoogle = "OIDCGoogle"
	FlavorIdentOIDCAzure  = "OIDCAzure"
)

// authFlavorIdents maps each AuthFlavor identifier to the login flavor it names (as the
// auth scan reports it).
var authFlavorIdents = map[string]string{
	FlavorIdentPassword:   FlavorPassword,
	FlavorIdentOIDCGoogle: FlavorOIDCGoogle,
	FlavorIdentOIDCAzure:  FlavorOIDCAzure,
}

// FlavorIdent returns the generation package's AuthFlavor identifier for a login flavor,
// or empty for a flavor the generated router does not compose (preauth).
func FlavorIdent(flavor string) string {
	for ident, f := range authFlavorIdents {
		if f == flavor {
			return ident
		}
	}

	return ""
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
	optAuth:             {kind: kindOutletOption, params: []paramKind{paramString, paramFlavor}},
	optAPIKey:           {kind: kindOutletOption},
	optWebApp:           {kind: kindOutletOption, params: []paramKind{paramString}},
	optOldestAnswered:   {kind: kindOutletOption, params: []paramKind{paramRelease}},
	optOutletRequestLog: {kind: kindOutletOption, params: []paramKind{paramRequestLog}},
	optOutletTraces:     {kind: kindOutletOption, params: []paramKind{paramTraces}},

	"ForOutlet":           {kind: kindTSOption, params: []paramKind{paramString}},
	"GeneratePermissions": {kind: kindTSOption},
	"GenerateMetadata":    {kind: kindTSOption},
	"GenerateEnums":       {kind: kindTSOption},
}
