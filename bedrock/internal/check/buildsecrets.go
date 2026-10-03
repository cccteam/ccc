// buildsecrets.go checks the build-time secrets: a secret the Dockerfile mounts as
// required must be declared in every environment, or a release that passed the earlier
// environments fails in the image build of the one that declares it not.

package check

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/secret"
)

// BuildSecretFinding is a build secret the Dockerfile mounts as required that some
// environment does not declare.
type BuildSecretFinding struct {
	// ID is the mount's id, the secret's name.
	ID string
	// Missing lists the environments whose build_secrets declare no such secret, in
	// promotion order.
	Missing []string
}

// mountRE matches one --mount flag of a RUN instruction.
var mountRE = regexp.MustCompile(`--mount=(\S+)`)

// scanBuildSecrets reads the Dockerfile at the application root for the secrets its
// RUN instructions mount as required (--mount=type=secret,id=NAME,required=true) and
// the placement in the stack directory for the build secrets each environment declares,
// and reports every required mount some environment lacks. An optional mount
// (required=false, or no required) passes with nothing said; so does an application
// without a Dockerfile.
func scanBuildSecrets(appDir, dir string, envs []string) ([]BuildSecretFinding, error) {
	src, err := os.ReadFile(filepath.Join(appDir, "Dockerfile"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, errors.Wrap(err, "os.ReadFile(): Dockerfile")
	}
	required := requiredMounts(src)
	if len(required) == 0 {
		return nil, nil
	}
	declared := map[string][]string{}
	for _, env := range envs {
		names, err := secret.BuildSecretNames(dir, env)
		if err != nil {
			return nil, err
		}
		declared[env] = names
	}
	var findings []BuildSecretFinding
	for _, id := range required {
		var missing []string
		for _, env := range envs {
			if !slices.Contains(declared[env], id) {
				missing = append(missing, env)
			}
		}
		if len(missing) > 0 {
			findings = append(findings, BuildSecretFinding{ID: id, Missing: missing})
		}
	}

	return findings, nil
}

// requiredMounts lists the ids of the secret mounts the Dockerfile's instructions mark
// required, sorted and unique. A comment line is not an instruction: the seeded
// Dockerfile shows the form in one.
func requiredMounts(dockerfile []byte) []string {
	var ids []string
	scanner := bufio.NewScanner(bytes.NewReader(dockerfile))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") {
			continue
		}
		for _, m := range mountRE.FindAllStringSubmatch(line, -1) {
			if id, ok := requiredSecret(m[1]); ok && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	slices.Sort(ids)

	return ids
}

// requiredSecret reads one mount's options and answers the secret's id when the mount
// is a required secret: type=secret, an id, and required (bare or =true).
func requiredSecret(options string) (string, bool) {
	var kind, id string
	required := false
	for _, option := range strings.Split(options, ",") {
		key, value, _ := strings.Cut(option, "=")
		switch key {
		case "type":
			kind = value
		case "id":
			id = value
		case "required":
			required = value == "" || strings.EqualFold(value, "true")
		}
	}
	if kind != "secret" || id == "" || !required {
		return "", false
	}

	return id, true
}
