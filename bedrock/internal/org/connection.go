// connection.go reads the two values of 2-env's placement that the Cloud Build GitHub
// connection needs: the app's installation and the token secret's version, which a
// browser step produces once per organization, before the first application is
// registered (org register refuses while either is unset).

package org

import (
	"os"
	"path/filepath"

	"github.com/go-playground/errors/v5"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// EnvTfvars is 2-env's placement, relative to the repository root.
const EnvTfvars = envLayer + "/" + tfvarsFile

// ConnectionValues are the attributes of 2-env's placement the connection needs, in
// the order the README sets them.
var ConnectionValues = []string{"github_app_installation_id", "github_oauth_token_secret_version"}

// ConnectionUnset names the connection values 2-env's placement under dir leaves unset
// (absent, commented out or null); none when both are set.
func ConnectionUnset(dir string) ([]string, error) {
	src, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(EnvTfvars)))
	if err != nil {
		return nil, errors.Wrapf(err, "os.ReadFile(): %s", EnvTfvars)
	}
	f, diags := hclsyntax.ParseConfig(src, EnvTfvars, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, errors.Wrapf(diags, "hclsyntax.ParseConfig(): %s", EnvTfvars)
	}
	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		return nil, errors.Newf("%s: not native HCL syntax", EnvTfvars)
	}
	var unset []string
	for _, name := range ConnectionValues {
		attr, ok := body.Attributes[name]
		if !ok {
			unset = append(unset, name)

			continue
		}
		v, diags := attr.Expr.Value(nil)
		if diags.HasErrors() {
			return nil, errors.Wrapf(diags, "%s in %s", name, EnvTfvars)
		}
		if v.IsNull() {
			unset = append(unset, name)
		}
	}

	return unset, nil
}
