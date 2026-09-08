package check

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-playground/errors/v5"
)

// paging finds list callers that still position a page by offset. The resource
// package positions pages by the cursor its Link header carries: the generated query
// builders have no Offset method and the server refuses the offset parameter, so a
// hand-written Go caller or a browser request that still sends one fails at compile
// time or with a 400. Reporting them here finds them before the upgrade does.
type paging struct{}

func (paging) Name() string { return "paging" }

func (paging) Describe() string {
	return "no application code positions a list by offset; pages are positioned by the server's cursor (Link header)"
}

// goOffsetRE matches the retired query-builder calls and the retired parameter name in Go.
var goOffsetRE = regexp.MustCompile(`\.Offset\(|\bSetOffset\(|"offset"`)

// webOffsetRE matches an offset query parameter being assembled in browser code: an
// offset key in a params object or query string, or the ListQuery field.
var webOffsetRE = regexp.MustCompile(`(?:['"]offset['"]\s*[:\],]|\boffset\s*:|[?&]offset=)`)

// webSourceExts are the browser source files the check reads.
var webSourceExts = map[string]bool{".ts": true, ".html": true, ".js": true}

func (c paging) Run(_ context.Context, env *Env) Result {
	a := env.App
	var details []string

	for _, rel := range a.GoFiles() {
		if strings.HasPrefix(path.Base(rel), "zz_gen_") {
			continue
		}
		src, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			return fail(c.Name(), errors.Wrapf(err, "reading %s", rel).Error())
		}
		details = append(details, matchLines(rel, src, goOffsetRE, "positions a list by offset; pages are positioned by the cursor in the Link header")...)
	}

	for _, w := range a.WebApps {
		var sources []string
		err := filepath.WalkDir(a.Abs(w.Dir), func(abs string, d fs.DirEntry, err error) error {
			if err != nil {
				return errors.Wrap(err, "filepath.WalkDir()")
			}
			if d.IsDir() {
				if skipped[d.Name()] {
					return filepath.SkipDir
				}

				return nil
			}
			if webSourceExts[filepath.Ext(d.Name())] && !strings.HasPrefix(d.Name(), "zz_gen_") {
				sources = append(sources, a.Rel(abs))
			}

			return nil
		})
		if err != nil {
			return fail(c.Name(), errors.Wrapf(err, "walking %s", w.Dir).Error())
		}
		for _, rel := range sources {
			src, err := os.ReadFile(a.Abs(rel))
			if err != nil {
				return fail(c.Name(), errors.Wrapf(err, "reading %s", rel).Error())
			}
			details = append(details, matchLines(rel, src, webOffsetRE, "sends an offset parameter; follow the Link header (page() in @cccteam/resource) instead")...)
		}
	}

	if len(details) > 0 {
		return warn(c.Name(), fmt.Sprintf("%d offset use(s) to move to cursors", len(details)), details...)
	}

	return pass(c.Name(), "no list is positioned by offset")
}

// matchLines reports every line of src the pattern matches as "file:line: message".
func matchLines(rel string, src []byte, re *regexp.Regexp, message string) []string {
	var out []string
	for i, line := range strings.Split(string(src), "\n") {
		if re.MatchString(line) {
			out = append(out, fmt.Sprintf("%s:%d: %s", rel, i+1, message))
		}
	}

	return out
}
