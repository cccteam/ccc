// framework.go lists the framework's settings structs: the structs a framework module
// declares with env tags, which an application embeds in a configuration struct instead
// of declaring the variables itself. The reader expands an embedded one into the fields
// the table lists, as if the struct declared them.

package derive

// frameworkKey names a framework settings struct: its import path and type name.
type frameworkKey struct {
	path string
	name string
}

// frameworkField is one env-tagged field of a framework settings struct.
type frameworkField struct {
	name     string
	typeName string
	// tag is the env tag's value as the framework writes it.
	tag string
	// doc is the field's doc comment as the framework writes it, one line per source
	// line, the way the parser reads a declared field's.
	doc string
}

// frameworkSettings are the framework's settings structs, by import path and type name,
// with the fields each declares in their order.
//
// Each entry mirrors the framework struct's declaration: the field names, env tags and
// doc comments as the framework module writes them. The cloud module's test holds that
// struct to these names, so a field renamed or retagged there fails there, and never
// only in a derived stack.
var frameworkSettings = map[frameworkKey][]frameworkField{
	{path: "github.com/cccteam/ccc/cloud/gcp", name: "Settings"}: {
		{
			name:     "LoggingProject",
			typeName: "string",
			tag:      "GOOGLE_CLOUD_LOGGING_PROJECT",
			doc: "LoggingProject is the Google Cloud project request logs ship to and spans are\n" +
				"recorded in. Empty logs to the console and exports no span: development.",
		},
		{
			name:     "TraceSampling",
			typeName: "string",
			tag:      "APP_TRACE_SAMPLING,default=edge",
			doc: "TraceSampling says which spans are recorded: every one (all), or the ones a request\n" +
				"Google's edge sampled starts (edge).",
		},
	},
}
