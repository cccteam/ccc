package check

import (
	"context"
	"fmt"
	"strings"
)

// outletShape verifies the one shape a browser outlet takes: its API prefix sits under its
// browser application's mount path, <mount>/api, which is api for an application at the
// root. The generator accepts any prefix and an outlet shaped otherwise works, so the check
// warns rather than fails: an application built before the guidance is tolerated, not a
// permanent exception. The generator holds the mount rule itself, refusing a browser
// application at / beside another, since an installed application's scope is every URL
// under its start.
type outletShape struct{}

func (outletShape) Name() string { return "outlet-shape" }

func (outletShape) Describe() string {
	return "every session outlet serving a browser application has its API prefix under the application's mount path (<mount>/api; api at the root)"
}

// apiSegment is the last segment of a browser outlet's API prefix in the one shape.
const apiSegment = "api"

func (c outletShape) Run(_ context.Context, env *Env) Result {
	p := env.App.Profile()
	if len(p.Sites) == 0 {
		return skip(c.Name(), "no site generator")
	}

	var details []string
	served := 0
	for i := range p.Sites {
		for _, o := range p.Sites[i].AllOutlets() {
			if o.WebApp == "" || !o.ServesSessions {
				continue
			}
			served++
			wanted := shapedPrefix(o.WebApp)
			if o.Prefix == wanted {
				continue
			}
			details = append(details, fmt.Sprintf("%s: outlet %s serves its API under /%s and its browser application at %s; the one shape puts an outlet's API under its application's mount path, /%s", o.Pos, o.Name, o.Prefix, o.WebApp, wanted))
		}
	}
	if served == 0 {
		return skip(c.Name(), "no outlet serves a browser application")
	}
	if len(details) > 0 {
		return warn(c.Name(), fmt.Sprintf("%d browser outlet(s) serve their API outside their application's mount path", len(details)), details...)
	}

	return pass(c.Name(), fmt.Sprintf("%d browser application(s) serve their API under their mount path", served))
}

// shapedPrefix is the API prefix the one shape gives a browser application at the mount
// path, without slashes at either end: api at the root, <mount>/api elsewhere.
func shapedPrefix(mount string) string {
	trimmed := strings.Trim(mount, "/")
	if trimmed == "" {
		return apiSegment
	}

	return trimmed + "/" + apiSegment
}
