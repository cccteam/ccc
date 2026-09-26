// Package where finds, for a command run from anywhere inside a repository with nothing
// typed, what the command line used to spell out: the repository, its infrastructure
// root, the application layer, the environments the layer pins secrets for, the secret
// variables the application declares, and, in Google Cloud, the environment project, the
// secret container and its versions. Each answer is either the one thing found or an
// error that lists the candidates, so a command can ask a person to choose or say what to
// pass.
package where

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/secret"
	"github.com/cccteam/ccc/impulse/app"
)

const (
	// gitDir marks a repository root: a directory, or a file pointing at one (a
	// worktree).
	gitDir = ".git"
	// infrastructureDir is where a repository keeps its infrastructure when the root
	// holds the application.
	infrastructureDir = "infrastructure"
	// orgLayer and appLayers are the two directories an infrastructure root holds: the
	// organization layer, and the application layers, one directory per application.
	orgLayer  = "1-org"
	appLayers = "3-app"
	// tfvarsFile is a layer's placement file; a directory under 3-app holding one is an
	// application layer.
	tfvarsFile = "terraform.tfvars"
	// localsFile is the layer's locals, whose secrets local lists the secret variables
	// when the application's code is not at hand.
	localsFile = "locals.tf"
	// placementFile is the placement beside the stack, which deriving the model needs.
	placementFile = "placement.json"
	// localsBlock and secretsLocal name the secrets local in locals.tf.
	localsBlock  = "locals"
	secretsLocal = "secrets"
)

// RepoRoot walks up from start to the directory holding .git (a directory, or the file
// a worktree carries) and returns it.
func RepoRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", errors.Wrap(err, "filepath.Abs()")
	}
	for {
		if _, err := os.Lstat(filepath.Join(dir, gitDir)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.Newf("no repository above %s (no %s in it or any directory above): run inside the repository, or pass --dir", start, gitDir)
		}
		dir = parent
	}
}

// InfrastructureRoot is the directory holding the layers (1-org and 3-app): the
// repository root itself, or its infrastructure directory.
func InfrastructureRoot(repoRoot string) (string, error) {
	candidates := []string{repoRoot, filepath.Join(repoRoot, infrastructureDir)}
	for _, dir := range candidates {
		if holdsLayers(dir) {
			return dir, nil
		}
	}

	return "", errors.Newf("neither %s nor %s holds the layers (%s and %s): pass --dir with the infrastructure root", candidates[0], candidates[1], orgLayer, appLayers)
}

// holdsLayers reports whether the directory holds the organization layer and the
// application layers.
func holdsLayers(dir string) bool {
	for _, layer := range []string{orgLayer, appLayers} {
		info, err := os.Stat(filepath.Join(dir, layer))
		if err != nil || !info.IsDir() {
			return false
		}
	}

	return true
}

// ApplicationDir is the application's source directory when the layout says where it
// is: the repository root when the infrastructure lives in its infrastructure directory.
// Otherwise it is unknown, and empty.
func ApplicationDir(repoRoot, infraRoot string) string {
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return ""
	}
	infraRoot, err = filepath.Abs(infraRoot)
	if err != nil {
		return ""
	}
	if infraRoot == filepath.Join(repoRoot, infrastructureDir) {
		return repoRoot
	}

	return ""
}

// Applications lists the application layers under the infrastructure root: the
// directories under 3-app holding a terraform.tfvars, sorted.
func Applications(infraRoot string) ([]string, error) {
	dir := filepath.Join(infraRoot, appLayers)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.Newf("no %s under %s: --dir is the infrastructure root, the directory holding the layers", appLayers, infraRoot)
		}

		return nil, errors.Wrap(err, "os.ReadDir()")
	}
	var apps []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, entry.Name(), tfvarsFile)); err != nil {
			continue
		}
		apps = append(apps, entry.Name())
	}
	sort.Strings(apps)

	return apps, nil
}

// SingleApplication is the one application layer under the infrastructure root. Several
// and none are refused, the several listed.
func SingleApplication(infraRoot string) (string, error) {
	apps, err := Applications(infraRoot)
	if err != nil {
		return "", err
	}
	switch len(apps) {
	case 0:
		return "", errors.Newf("no application layer under %s (a directory holding a %s)", filepath.Join(infraRoot, appLayers), tfvarsFile)
	case 1:
		return apps[0], nil
	default:
		return "", errors.Newf("several application layers under %s (%s): pass --app", filepath.Join(infraRoot, appLayers), strings.Join(apps, ", "))
	}
}

// LayerDir is the application's layer under the infrastructure root.
func LayerDir(infraRoot, application string) string {
	return filepath.Join(infraRoot, appLayers, application)
}

// Environments lists the environments the application's placement pins secrets for: the
// keys of secret_versions in its terraform.tfvars, in the file's order.
func Environments(infraRoot, application string) ([]string, error) {
	return LayerEnvironments(LayerDir(infraRoot, application))
}

// LayerEnvironments is Environments for a layer named by its directory, when the layer
// is not the application's own (--layer).
func LayerEnvironments(layerDir string) ([]string, error) {
	return secret.PlacementEnvironments(filepath.Join(layerDir, tfvarsFile))
}

// Variables lists the secret variables the application declares. With the application's
// source directory known, they are read from the code the way render reads it: the model
// derived from the code and the placement beside the layer, and its secrets in
// declaration order. Without it, or when the code cannot be read that way, they are the
// keys of the secrets local in the layer's locals.tf, which render wrote from the same
// model.
func Variables(appDir, layerDir string) ([]string, error) {
	if appDir == "" {
		return localsSecrets(filepath.Join(layerDir, localsFile))
	}
	names, err := derivedSecrets(appDir, layerDir)
	if err == nil {
		return names, nil
	}
	names, fallbackErr := localsSecrets(filepath.Join(layerDir, localsFile))
	if fallbackErr != nil {
		return nil, errors.Newf("%s (and reading the code at %s: %s)", errors.Cause(fallbackErr), appDir, errors.Cause(err))
	}

	return names, nil
}

// derivedSecrets reads the secret variables from the application's code, through the
// model derived with the placement beside the layer.
func derivedSecrets(appDir, layerDir string) ([]string, error) {
	a, err := app.Discover(appDir)
	if err != nil {
		return nil, err
	}
	p, err := derive.ReadPlacement(filepath.Join(layerDir, placementFile))
	if err != nil {
		return nil, err
	}
	m, err := derive.Derive(a, p)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(m.Secrets))
	for _, s := range m.Secrets {
		names = append(names, s.Variable.Name)
	}

	return names, nil
}

// localsSecrets reads the keys of the secrets local in the locals file, in the file's
// order.
func localsSecrets(file string) ([]string, error) {
	src, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.Newf("no %s: the application layer's locals list the secret variables the code declares", file)
		}

		return nil, errors.Wrap(err, "os.ReadFile()")
	}
	f, diags := hclsyntax.ParseConfig(src, file, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, errors.Wrap(diags, "hclsyntax.ParseConfig()")
	}
	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		return nil, errors.Newf("%s: not native HCL syntax", file)
	}
	for _, block := range body.Blocks {
		if block.Type != localsBlock {
			continue
		}
		attr, ok := block.Body.Attributes[secretsLocal]
		if !ok {
			continue
		}
		obj, ok := attr.Expr.(*hclsyntax.ObjectConsExpr)
		if !ok {
			return nil, errors.Newf("%s in %s is not a map written out ({ ... })", secretsLocal, file)
		}
		names := make([]string, 0, len(obj.Items))
		for _, item := range obj.Items {
			name, err := secret.ObjectKey(item.KeyExpr)
			if err != nil {
				return nil, errors.Wrapf(err, "%s in %s", secretsLocal, file)
			}
			names = append(names, name)
		}

		return names, nil
	}

	return nil, errors.Newf("no %s local in %s: the application layer's locals list the secret variables the code declares", secretsLocal, file)
}
