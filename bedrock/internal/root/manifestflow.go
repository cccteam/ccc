// manifestflow.go is the browser part of the bootstrap: a page on the person's own
// computer with one button per GitHub App to make. The button posts the app's manifest
// to GitHub, where the person clicks Create once; GitHub sends the browser back with a
// code, which the page turns into the app and its private key; the page then offers the
// install link, and sees the installation once the person has clicked Install. No key
// touches the disk: it goes from GitHub's answer to Secret Manager.

package root

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

// Converter turns a manifest's code into the app; the GitHub client's ConvertManifest is
// the real one.
type Converter func(ctx context.Context, code string) (*github.App, error)

// Flow is one run of the browser flow: the apps to make, the ones made and the ones
// installed.
type Flow struct {
	mu           sync.Mutex
	server       *http.Server
	listener     net.Listener
	manifests    []org.AppManifest
	organization string
	homepage     string
	convert      Converter
	gh           deploy.GitHubFunc
	states       map[string]org.AppRole
	apps         map[org.AppRole]*github.App
	installed    map[org.AppRole]int64
	failures     []string
}

// flowPoll is how often the flow looks for the installations and the page refreshes.
const flowPoll = 3 * time.Second

// StartFlow serves the page on a port of the computer's own, for the apps not yet made
// (brought apps are left out), and answers the flow; its URL is where the person goes.
func StartFlow(ctx context.Context, manifests []org.AppManifest, organization, homepage string, convert Converter, gh deploy.GitHubFunc) (*Flow, error) {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, errors.Wrap(err, "net.ListenConfig.Listen()")
	}
	f := &Flow{listener: listener, manifests: manifests, organization: organization, homepage: homepage, convert: convert, gh: gh, states: map[string]org.AppRole{}, apps: map[org.AppRole]*github.App{}, installed: map[org.AppRole]int64{}}
	for _, m := range manifests {
		state, err := randomState()
		if err != nil {
			_ = listener.Close()

			return nil, err
		}
		f.states[state] = m.Role
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", f.page)
	mux.HandleFunc("/made", f.made)
	mux.HandleFunc("/installed", f.installedRedirect)
	f.server = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = f.server.Serve(listener) }()

	return f, nil
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", errors.Wrap(err, "rand.Read()")
	}

	return hex.EncodeToString(b), nil
}

// URL is the page's address on this computer.
func (f *Flow) URL() string {
	return "http://" + f.listener.Addr().String()
}

// Close stops the page.
func (f *Flow) Close() {
	_ = f.server.Close()
}

// Wait waits until every app is made and installed on the organization, looking every few
// seconds, and answers the apps with their keys; it stops with the context.
func (f *Flow) Wait(ctx context.Context) (map[org.AppRole]*github.App, error) {
	for {
		if err := f.lookForInstallations(ctx); err != nil {
			return nil, err
		}
		if f.done() {
			f.mu.Lock()
			apps := f.apps
			f.mu.Unlock()

			return apps, nil
		}
		select {
		case <-ctx.Done():
			return nil, errors.Wrap(ctx.Err(), "waiting for the apps to be made and installed in the browser")
		case <-time.After(flowPoll):
		}
	}
}

// done reports whether every app is made and installed.
func (f *Flow) done() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.manifests {
		if f.apps[m.Role] == nil || f.installed[m.Role] == 0 {
			return false
		}
	}

	return true
}

// lookForInstallations asks GitHub, as each made app, whether it is installed on the
// organization yet.
func (f *Flow) lookForInstallations(ctx context.Context) error {
	f.mu.Lock()
	var pending []org.AppRole
	for role, app := range f.apps {
		if app != nil && f.installed[role] == 0 {
			pending = append(pending, role)
		}
	}
	f.mu.Unlock()
	for _, role := range pending {
		f.mu.Lock()
		app := f.apps[role]
		f.mu.Unlock()
		installations, err := deploy.AppInstallations(ctx, f.gh, fmt.Sprint(app.ID), []byte(app.PEM))
		if err != nil {
			return err
		}
		for _, in := range installations {
			if in.Account.Login == f.organization {
				f.mu.Lock()
				f.installed[role] = in.ID
				f.mu.Unlock()
			}
		}
	}

	return nil
}

// row is one app on the page.
type row struct {
	Name, Title, Description string
	Form                     string
	Manifest                 string
	App                      *github.App
	Installed                bool
	InstallURL               string
}

var pageTemplate = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>bedrock org bootstrap: the GitHub Apps</title>
<meta http-equiv="refresh" content="{{.Refresh}}">
<style>body{font-family:system-ui,sans-serif;max-width:48em;margin:3em auto;padding:0 1em;line-height:1.5}
.app{border:1px solid #ccc;border-radius:6px;padding:1em 1.5em;margin:1em 0}.done{color:#2a7a2a}button,a.button{font:inherit;padding:.4em 1em}</style></head>
<body><h1>The GitHub Apps of the {{.Organization}} organization</h1>
<p>bedrock org bootstrap makes three GitHub Apps, owned by the <b>{{.Organization}}</b> organization. For each: <b>Create</b> opens GitHub with the app's manifest, where one click makes it; GitHub then brings you back here. <b>Install</b> opens the app's installation page: choose the organization and <b>all repositories</b>. This page refreshes on its own.</p>
{{range .Rows}}<div class="app"><h2>{{.Name}}</h2><p>{{.Description}}</p>
{{if not .App}}<form method="post" action="{{.Form}}"><input type="hidden" name="manifest" value="{{.Manifest}}"><button type="submit">Create {{.Name}} on GitHub</button></form>
{{else if not .Installed}}<p class="done">Made: App {{.App.ID}} ({{.App.Slug}}); its key is in bedrock's hands.</p><p><a class="button" href="{{.InstallURL}}">Install {{.Name}} on {{$.Organization}}, for all repositories</a></p>
{{else}}<p class="done">Made (App {{.App.ID}}) and installed on {{$.Organization}}.</p>{{end}}</div>
{{end}}{{if .Done}}<p class="done"><b>All three apps are made and installed. Back to the terminal: the bootstrap goes on by itself.</b></p>{{end}}
{{range .Failures}}<p style="color:#a00">{{.}}</p>{{end}}
</body></html>`))

// page renders the apps and their state.
func (f *Flow) page(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)

		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var rows []row
	for _, m := range f.manifests {
		state := f.stateOf(m.Role)
		manifest, err := json.Marshal(m.GitHub(f.homepage, f.URL()+"/made", f.URL()+"/installed"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}
		rw := row{Name: m.Name, Title: m.Role.Title(), Description: m.Description, Form: github.ManifestFormURL(f.organization, state), Manifest: string(manifest), App: f.apps[m.Role], Installed: f.installed[m.Role] != 0}
		if rw.App != nil {
			rw.InstallURL = "https://github.com/apps/" + rw.App.Slug + "/installations/new"
		}
		rows = append(rows, rw)
	}
	done := true
	for _, rw := range rows {
		if rw.App == nil || !rw.Installed {
			done = false
		}
	}
	refresh := int(flowPoll / time.Second)
	if done {
		refresh = 3600
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pageTemplate.Execute(w, map[string]any{"Organization": f.organization, "Rows": rows, "Done": done, "Failures": f.failures, "Refresh": refresh})
}

func (f *Flow) stateOf(role org.AppRole) string {
	for state, r := range f.states {
		if r == role {
			return state
		}
	}

	return ""
}

// made is where GitHub sends the browser with the code: the app is made from it.
func (f *Flow) made(w http.ResponseWriter, r *http.Request) {
	code, state := r.URL.Query().Get("code"), r.URL.Query().Get("state")
	f.mu.Lock()
	role, ok := f.states[state]
	f.mu.Unlock()
	if !ok || code == "" {
		http.Error(w, "this is not a redirect from GitHub's manifest flow for one of the three apps", http.StatusBadRequest)

		return
	}
	app, err := f.convert(r.Context(), code)
	f.mu.Lock()
	if err != nil {
		f.failures = append(f.failures, fmt.Sprintf("GitHub did not make %s from the code it sent: %v. Try Create again.", role.Title(), errors.Cause(err)))
	} else {
		f.apps[role] = app
	}
	f.mu.Unlock()
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// installedRedirect is where GitHub sends the browser after an installation: back to the
// page, which looks for the installation.
func (f *Flow) installedRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Roles are the roles of the apps the flow makes, in order.
func (f *Flow) Roles() []org.AppRole {
	roles := make([]org.AppRole, 0, len(f.manifests))
	for _, m := range f.manifests {
		roles = append(roles, m.Role)
	}

	return roles
}

// Pending names the apps not yet made and installed, for a message.
func (f *Flow) Pending() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var pending []string
	for _, m := range f.manifests {
		switch {
		case f.apps[m.Role] == nil:
			pending = append(pending, m.Name+" (to make)")
		case f.installed[m.Role] == 0:
			pending = append(pending, m.Name+" (to install)")
		}
	}
	slices.Sort(pending)

	return strings.Join(pending, ", ")
}
