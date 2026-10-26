package check

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
)

// The login page the skeletons carry, as a flat console's: the card bound to the
// component's arriving signal through a template reference, the stylesheet hiding the
// held card, and the component file naming the stylesheet.
const (
	heldPage = `<div class="login-page">
  <mat-card class="login-card" [class.arriving]="methods.arriving()" appearance="outlined">
    <mat-card-content>
      <ccc-login-methods #methods home="/dashboard" (signedIn)="go($event)" />
    </mat-card-content>
  </mat-card>
</div>
`
	heldStyles = `.login-page {
  display: flex;
}

.login-card.arriving {
  visibility: hidden;
}
`
	loginComponentFile = `import { Component } from '@angular/core';

@Component({
  selector: 'app-login',
  templateUrl: './login.component.html',
  styleUrls: ['./login.component.scss'],
})
export class LoginComponent {}
`
	consoleLogin = "web/console/src/app/components/login/login.component"
	portalLogin  = "web/portal/src/app/components/login/login.component"
)

// loginHeldFlat lays out the flat layout of the web-wired tests with a login page per
// browser project: the console on the default outlet and the portal on its outlet.
func loginHeldFlat() map[string]string {
	files := map[string]string{
		"pkg/auth/staff/staff.go":     webAuth("staff", true),
		"pkg/auth/members/members.go": webAuth("members", true),
		"cmd/generate/main.go": program("pkg/resources",
			`generation.GenerateHandlers("app"),`,
			`generation.GenerateRouter(),`,
			`generation.GenerateRoutes("pkg/router", "console/api", generation.Auth("example.com/harbor/pkg/auth/staff", generation.PasswordMethod()), generation.WebApp("/console")),`,
			`generation.WithRouterOutlet("portal", "portal/api", generation.Auth("example.com/harbor/pkg/auth/members", generation.PasswordMethod()), generation.WebApp("/portal")),`,
			`generation.GenerateTypescript("web/console/src/app/core/service"),`,
			`generation.GenerateTypescript("web/portal/src/app/core/service", generation.ForOutlet("portal")),`,
		),
		"web/angular.json": `{"projects": {"console": {"root": "console", "sourceRoot": "console/src"}, "portal": {"root": "portal", "sourceRoot": "portal/src"}}}` + "\n",
	}
	for _, p := range []string{consoleLogin, portalLogin} {
		files[p+".html"] = heldPage
		files[p+".scss"] = heldStyles
		files[p+".ts"] = loginComponentFile
	}

	return files
}

func TestLoginHeld(t *testing.T) {
	t.Parallel()

	const (
		consoleHeld = "web/console/src/app/components/login/login.component.html: <ccc-login-methods #methods> at line 4, methods.arriving() read at line 2, the arriving class hidden by web/console/src/app/components/login/login.component.scss"
		portalHeld  = "web/portal/src/app/components/login/login.component.html: <ccc-login-methods #methods> at line 4, methods.arriving() read at line 2, the arriving class hidden by web/portal/src/app/components/login/login.component.scss"
		shape       = `bind the card's arriving class to it ([class.arriving]="methods.arriving()" on the <mat-card class="login-card">), with .login-card.arriving { visibility: hidden } in the page's stylesheet, as the skeletons' login pages do`
	)

	tests := []struct {
		name        string
		edit        func(files map[string]string)
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name:        "the skeletons' pages hold their card",
			wantStatus:  Pass,
			wantSummary: "2 login page(s) hold their card back while the login component's arrival reads are in flight",
			wantDetails: []string{consoleHeld, portalHeld},
		},
		{
			name: "a page from before the signal gives the component no reference",
			edit: func(files map[string]string) {
				files[portalLogin+".html"] = strings.ReplaceAll(strings.ReplaceAll(heldPage, ` [class.arriving]="methods.arriving()"`, ""), "#methods ", "")
			},
			wantStatus:  Fail,
			wantSummary: "1 login page(s) show their card before the login component's arrival reads answer",
			wantDetails: []string{
				portalLogin + ".html:4: the login component carries no template reference, so the page cannot read its arriving signal and its card shows before the component knows what it renders; write <ccc-login-methods #methods ...> and " + shape,
			},
		},
		{
			name: "a reference the page never reads",
			edit: func(files map[string]string) {
				files[consoleLogin+".html"] = strings.ReplaceAll(heldPage, ` [class.arriving]="methods.arriving()"`, "")
			},
			wantStatus:  Fail,
			wantSummary: "1 login page(s) show their card before the login component's arrival reads answer",
			wantDetails: []string{
				consoleLogin + ".html:4: the page never reads methods.arriving(), so its card shows before the login component knows what it renders; " + shape,
			},
		},
		{
			name: "a class bound but hidden by no stylesheet",
			edit: func(files map[string]string) {
				files[consoleLogin+".scss"] = ".login-page {\n  display: flex;\n}\n"
			},
			wantStatus:  Fail,
			wantSummary: "1 login page(s) show their card before the login component's arrival reads answer",
			wantDetails: []string{
				consoleLogin + ".html:2: the page binds the arriving class to methods.arriving(), but no stylesheet of the page hides .arriving (read web/console/src/app/components/login/login.component.scss), so the card shows while the arrival reads are in flight; write .login-card.arriving { visibility: hidden } in the page's stylesheet",
			},
		},
		{
			name: "a page reading the signal another way holds its card",
			edit: func(files map[string]string) {
				files[consoleLogin+".html"] = "@if (!methods.arriving()) {\n  <mat-card class=\"login-card\">\n    <ccc-login-methods #methods home=\"/dashboard\" />\n  </mat-card>\n}\n"
			},
			wantStatus:  Pass,
			wantSummary: "2 login page(s) hold their card back while the login component's arrival reads are in flight",
			wantDetails: []string{
				"web/console/src/app/components/login/login.component.html: <ccc-login-methods #methods> at line 3, methods.arriving() read at line 1",
				portalHeld,
			},
		},
		{
			name: "inline styles hide the class",
			edit: func(files map[string]string) {
				delete(files, consoleLogin+".scss")
				files[consoleLogin+".ts"] = "import { Component } from '@angular/core';\n\n@Component({\n  selector: 'app-login',\n  templateUrl: './login.component.html',\n  styles: [`.login-card.arriving { visibility: hidden; }`],\n})\nexport class LoginComponent {}\n"
			},
			wantStatus:  Pass,
			wantSummary: "2 login page(s) hold their card back while the login component's arrival reads are in flight",
			wantDetails: []string{
				"web/console/src/app/components/login/login.component.html: <ccc-login-methods #methods> at line 4, methods.arriving() read at line 2, the arriving class hidden by the inline styles of web/console/src/app/components/login/login.component.ts",
				portalHeld,
			},
		},
		{
			name: "a browser application rendering the component nowhere is noted",
			edit: func(files map[string]string) {
				delete(files, portalLogin+".html")
				delete(files, portalLogin+".scss")
				delete(files, portalLogin+".ts")
			},
			wantStatus:  Warn,
			wantSummary: "1 browser application(s) render the library's login component nowhere",
			wantDetails: []string{
				"web/portal/src renders the library's login component (<ccc-login-methods>) nowhere, so whether its login page holds its card back is not read",
				consoleHeld,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := loginHeldFlat()
			if tt.edit != nil {
				tt.edit(files)
			}
			root := t.TempDir()
			write := func(rel, content string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write("go.mod", "module example.com/harbor\n\ngo 1.26.6\n")
			for rel, content := range files {
				write(rel, content)
			}

			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			got := loginHeld{}.Run(context.Background(), &Env{App: a})
			want := Result{Name: loginHeld{}.Name(), Status: tt.wantStatus, Summary: tt.wantSummary, Details: tt.wantDetails}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestLoginHeldMeaning(t *testing.T) {
	t.Parallel()

	meaning := loginHeld{}.Meaning()
	for _, want := range []string{"<ccc-login-methods #methods ...>", `[class.arriving]="methods.arriving()"`, ".login-card.arriving { visibility: hidden }", "@cccteam/resource-angular` 0.0.17"} {
		if !strings.Contains(meaning, want) {
			t.Errorf("Meaning() lacks %q", want)
		}
	}
}
