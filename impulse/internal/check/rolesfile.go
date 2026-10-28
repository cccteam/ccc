package check

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/cccteam/access"
	"github.com/go-playground/errors/v5"
)

// rolesFileAccepted verifies that every auth package's role file is one the permission
// engine accepts: the file parses the way the engine reads it when the release starts
// (access.RoleFile.Parse), which settles everything the file decides on its own, with no
// permission collection. Every role is named in the one grammar, letters and digits
// starting with a capital letter, with the scope never part of the name; every role
// carries a description saying what it is for; and every grant names a resource, with a
// condition that parses. A file the engine would refuse stops the release at start, so
// the refusal is a finding here, naming the file, the role and the rule. What the file
// grants is validated against the collection by the engine and the validation test
// auths-wired asks for; this reads the file alone.
type rolesFileAccepted struct{}

// rolesFileAcceptedName is the check's name.
const rolesFileAcceptedName = "roles-file"

func (rolesFileAccepted) Name() string {
	return rolesFileAcceptedName
}

func (rolesFileAccepted) Describe() string {
	return "every auth package's role file (pkg/auth/<name>/roles.json) parses the way the permission engine reads it at start, with no permission collection: every role is named in the one grammar (letters and digits starting with a capital letter; the scope is never part of the name) and carries a description saying what it is for, and every grant names a resource with a condition that parses; a file the engine would refuse is a finding naming the file, the role and the rule"
}

func (c rolesFileAccepted) Run(_ context.Context, env *Env) Result {
	a := env.App
	if len(a.AuthPackages) == 0 {
		return skip(c.Name(), "no auth package (pkg/auth/<name>) holds a role file")
	}

	var details, files []string
	for i := range a.AuthPackages {
		p := &a.AuthPackages[i]
		rel := rolesFileOf(p)
		data, err := os.ReadFile(a.Abs(rel))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			details = append(details, fmt.Sprintf("%s does not exist; the %s auth embeds its role file from there", rel, p.Name))

			continue
		case err != nil:
			details = append(details, fmt.Sprintf("%s: cannot be read: %v", rel, errors.Cause(err)))

			continue
		}
		if _, err := access.RoleFile(data).Parse(); err != nil {
			details = append(details, fmt.Sprintf("%s: %s", rel, refusalWords(err)))

			continue
		}
		files = append(files, rel)
	}
	if len(details) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d role file(s) the permission engine would refuse at start", len(details)), details...)
	}

	return pass(c.Name(), fmt.Sprintf("%d role file(s) parse the way the permission engine reads them: %s", len(files), strings.Join(files, ", ")))
}

// refusalWords is a refusal's words alone: the outermost prefix the error chain carries,
// when it carries one, and the cause, without the source locations the chain renders.
func refusalWords(err error) string {
	var chain errors.Chain
	if !errors.As(err, &chain) {
		return err.Error()
	}
	cause := errors.Cause(err).Error()
	for i := len(chain) - 1; i >= 0; i-- {
		if chain[i].Prefix != "" {
			return chain[i].Prefix + ": " + cause
		}
	}

	return cause
}
