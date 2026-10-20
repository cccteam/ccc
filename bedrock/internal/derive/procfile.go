// procfile.go reads the development process file at the application root, overmind's
// Procfile: one process per line, <name>: <command>, with # comments between. In the
// sites layout each site's process line sets the site's bundle directory inline
// (APP_DIST=apps/console/web/dist/console go run ./apps/console), the one value of
// development the stack repeats per service: the image copies that directory out of the
// workspace's build, and the service serves it from under the image's working directory.

package derive

import (
	"os"
	"regexp"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// ProcfileName is the development process file at the application root.
const ProcfileName = "Procfile"

// processLineRE is a process line: the name, the colon, the command.
var processLineRE = regexp.MustCompile(`^([A-Za-z0-9_-]+):\s*(.*)$`)

// procfile is the development process file's processes by name, each its command; nil
// for an application without one.
type procfile map[string]string

// readProcfile reads the Procfile at the application root, or nothing when the
// application has none.
func readProcfile(a *app.App) (procfile, error) {
	src, err := os.ReadFile(a.Abs(ProcfileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, errors.Wrapf(err, "os.ReadFile(): %s", ProcfileName)
	}

	return parseProcfile(string(src)), nil
}

// parseProcfile reads the processes off the file's text: a line naming a process and its
// command, a comment or a blank line skipped, and anything else left alone.
func parseProcfile(src string) procfile {
	processes := procfile{}
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := processLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		processes[m[1]] = m[2]
	}

	return processes
}

// env is the value the process's command sets the variable to inline, the VAR=value word
// before a program as a shell reads it, the value bare or in single or double quotes, or
// empty when the Procfile has no such process or its command sets none.
func (p procfile) env(process, variable string) string {
	assignment := regexp.MustCompile(`(?:^|[\s'"])` + regexp.QuoteMeta(variable) + `=(?:"([^"]*)"|'([^']*)'|([^\s'"]+))`)
	m := assignment.FindStringSubmatch(p[process])
	if m == nil {
		return ""
	}
	for _, value := range m[1:] {
		if value != "" {
			return value
		}
	}

	return ""
}
