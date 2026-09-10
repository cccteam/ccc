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
)

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
	case paramNone, paramAny, paramString, paramBool, paramStringMap, paramBoolMap, paramComposite, paramFlavor:
		return 0
	default:
		return 0
	}
}

// Option constructor names the profile reads beyond the positional accessors.
const (
	optServesSessions     = "ServesSessions"
	optGenerateHandlers   = "GenerateHandlers"
	optGenerateRoutes     = "GenerateRoutes"
	optGenerateRouter     = "GenerateRouter"
	optGenerateTypescript = "GenerateTypescript"
	optAuth               = "Auth"
	optAPIKey             = "APIKey"
	optWebApp             = "WebApp"
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
	"WithDomainRoute":            {kind: kindResourceOption, params: []paramKind{paramString}},
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

	optServesSessions: {kind: kindOutletOption},
	optAuth:           {kind: kindOutletOption, params: []paramKind{paramString, paramFlavor}},
	optAPIKey:         {kind: kindOutletOption},
	optWebApp:         {kind: kindOutletOption, params: []paramKind{paramString}},

	"WithTypescriptOverrides": {kind: kindTSOption, params: []paramKind{paramStringMap}},
	"ForOutlet":               {kind: kindTSOption, params: []paramKind{paramString}},
	"GeneratePermissions":     {kind: kindTSOption},
	"GenerateMetadata":        {kind: kindTSOption},
	"GenerateEnums":           {kind: kindTSOption},
}
