// framework.go lists the framework's settings structs: the structs a framework module
// declares with env tags, which an application embeds in a configuration struct instead
// of declaring the variables itself. The reader expands an embedded one into the fields
// its declaration lists, as if the struct declared them.

package derive

import (
	"github.com/cccteam/ccc/cloud"
	"github.com/cccteam/ccc/cloud/gcp"
)

// frameworkSettings are the framework's settings structs, as the modules declaring them
// export their declarations: the cloud driver's. The declaring module's test holds its
// declaration to the struct, so a field renamed, retagged or re-documented there fails
// there, and never only in a derived stack.
var frameworkSettings = []cloud.Declaration{gcp.SettingsDeclaration()}

// frameworkSetting finds the settings struct an embedded type names, by the import path
// the file resolves its package to and the type name.
func frameworkSetting(importPath, typeName string) (cloud.Declaration, bool) {
	for _, d := range frameworkSettings {
		if d.Path == importPath && d.Name == typeName {
			return d, true
		}
	}

	return cloud.Declaration{}, false
}
