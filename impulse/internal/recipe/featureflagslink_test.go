package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
)

// soloHeader is the skeleton's header class before the link, as the solo candidate's.
const soloHeader = `import { Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { RouterModule } from '@angular/router';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { TopbarComponent } from '../topbar/topbar.component';

@Component({
  selector: 'app-header',
  imports: [MatButtonModule, RouterModule, TopbarComponent],
  templateUrl: './header.component.html',
  styleUrls: ['./header.component.scss'],
})
export class HeaderComponent {
  auth = inject(AuthService);
}
`

// soloHeaderLinked is soloHeader with the link, as the recipe writes it.
const soloHeaderLinked = `import { Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatDialog } from '@angular/material/dialog';
import { RouterModule } from '@angular/router';
import { Permissions, Resources } from '@app/service/zz_gen_constants';
import { PermissionScope } from '@cccteam/resource';
import { HasPermissionDirective } from '@cccteam/resource-angular/auth-has-permission';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { openFeatureFlagsDialog } from '@cccteam/resource-angular/ccc-feature-flags';
import { TopbarComponent } from '../topbar/topbar.component';

@Component({
  selector: 'app-header',
  imports: [HasPermissionDirective, MatButtonModule, RouterModule, TopbarComponent],
  templateUrl: './header.component.html',
  styleUrls: ['./header.component.scss'],
})
export class HeaderComponent {
  auth = inject(AuthService);
  private dialog = inject(MatDialog);

  /**
   * The grant that shows the Feature flags link: List on the flags, which is what the
   * library's dialog reads. Whether the dialog may flip one is the digest's Execute on
   * SetFeature, which the dialog asks for itself; with List alone it is read-only.
   */
  readonly flagsScope: PermissionScope = { resource: Resources.FeatureFlags, permission: Permissions.List };

  /** Opens the library's feature flags dialog over the application. */
  openFeatureFlags(): void {
    openFeatureFlagsDialog(this.dialog);
  }
}
`

// tenantedHeader is the tenanted candidate's header class: a second injection and the
// tenant picker's modules, which the imports keep their order around.
const tenantedHeader = `import { Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatSelectModule } from '@angular/material/select';
import { RouterModule } from '@angular/router';
import { TenantService } from '@app/tenant/tenant.service';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { TopbarComponent } from '../topbar/topbar.component';

@Component({
  selector: 'app-header',
  imports: [MatButtonModule, MatFormFieldModule, MatSelectModule, RouterModule, TopbarComponent],
  templateUrl: './header.component.html',
  styleUrls: ['./header.component.scss'],
})
export class HeaderComponent {
  auth = inject(AuthService);
  tenants = inject(TenantService);
}
`

// soloTemplate is the skeleton's header template before the link.
const soloTemplate = `<div class="header">
  <a class="brand" [routerLink]="['']">Solo</a>
  <app-topbar></app-topbar>
  <div class="session">
    <span class="username">{{ auth.sessionInfo().username }}</span>
    <a mat-stroked-button routerLink="/logout">Sign out</a>
  </div>
</div>
`

// soloTemplateLinked is soloTemplate with the button before the username.
const soloTemplateLinked = `<div class="header">
  <a class="brand" [routerLink]="['']">Solo</a>
  <app-topbar></app-topbar>
  <div class="session">
    <!--
      The feature flags dialog is the library's; the link shows under List on FeatureFlags,
      the dialog's read, and the digest decides inside the dialog whether the toggles work.
    -->
    <button mat-stroked-button class="flags" *cccHasPermission="flagsScope" (click)="openFeatureFlags()">
      Feature flags
    </button>
    <span class="username">{{ auth.sessionInfo().username }}</span>
    <a mat-stroked-button routerLink="/logout">Sign out</a>
  </div>
</div>
`

// customTemplate is a header an application reshaped: no username span to go before.
const customTemplate = `<nav class="bar">
  <a class="brand" [routerLink]="['']">Solo</a>
  <a mat-stroked-button routerLink="/logout">Sign out</a>
</nav>
`

func TestFeatureFlagsLink(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		files      map[string]string
		wantDetect []string
		wantDid    []string
		wantSkip   []string
		wantFiles  map[string]string
	}{
		{
			name: "the skeleton's header takes the link in its class and its template",
			files: map[string]string{
				"web/console/src/app/components/shared/header/header.component.ts":   soloHeader,
				"web/console/src/app/components/shared/header/header.component.html": soloTemplate,
			},
			wantDetect: []string{"web/console/src/app/components/shared/header/header.component.html: the header has no Feature flags link, so a feature administrator has no way to open the dialog"},
			wantDid: []string{
				"web/console/src/app/components/shared/header/header.component.ts: the header injects MatDialog, declares flagsScope (List on FeatureFlags) and openFeatureFlags(), and imports the directive and the dialog",
				"web/console/src/app/components/shared/header/header.component.html: the Feature flags button under *cccHasPermission=\"flagsScope\" before the username",
			},
			wantFiles: map[string]string{
				"web/console/src/app/components/shared/header/header.component.ts":   soloHeaderLinked,
				"web/console/src/app/components/shared/header/header.component.html": soloTemplateLinked,
			},
		},
		{
			name: "a second injection and more modules keep their places, and the members follow the last injection",
			files: map[string]string{
				"web/console/src/app/components/shared/header/header.component.ts":   tenantedHeader,
				"web/console/src/app/components/shared/header/header.component.html": soloTemplate,
			},
			wantDetect: []string{"web/console/src/app/components/shared/header/header.component.html: the header has no Feature flags link, so a feature administrator has no way to open the dialog"},
			wantDid: []string{
				"web/console/src/app/components/shared/header/header.component.ts: the header injects MatDialog, declares flagsScope (List on FeatureFlags) and openFeatureFlags(), and imports the directive and the dialog",
				"web/console/src/app/components/shared/header/header.component.html: the Feature flags button under *cccHasPermission=\"flagsScope\" before the username",
			},
			wantFiles: map[string]string{
				"web/console/src/app/components/shared/header/header.component.ts": strings.NewReplacer(
					"import { MatButtonModule } from '@angular/material/button';\n", "import { MatButtonModule } from '@angular/material/button';\nimport { MatDialog } from '@angular/material/dialog';\n",
					"import { TenantService } from '@app/tenant/tenant.service';\n", "import { Permissions, Resources } from '@app/service/zz_gen_constants';\nimport { TenantService } from '@app/tenant/tenant.service';\nimport { PermissionScope } from '@cccteam/resource';\nimport { HasPermissionDirective } from '@cccteam/resource-angular/auth-has-permission';\n",
					"import { AuthService } from '@cccteam/resource-angular/auth-service';\n", "import { AuthService } from '@cccteam/resource-angular/auth-service';\nimport { openFeatureFlagsDialog } from '@cccteam/resource-angular/ccc-feature-flags';\n",
					"  imports: [MatButtonModule,", "  imports: [HasPermissionDirective, MatButtonModule,",
					"  tenants = inject(TenantService);\n", "  tenants = inject(TenantService);\n"+headerFlagsMembers,
				).Replace(tenantedHeader),
			},
		},
		{
			name: "two browser applications are both edited, in path order",
			files: map[string]string{
				"apps/console/web/src/app/components/shared/header/header.component.ts":   soloHeader,
				"apps/console/web/src/app/components/shared/header/header.component.html": soloTemplate,
				"apps/portal/web/src/app/components/shared/header/header.component.ts":    soloHeader,
				"apps/portal/web/src/app/components/shared/header/header.component.html":  soloTemplate,
			},
			wantDetect: []string{
				"apps/console/web/src/app/components/shared/header/header.component.html: the header has no Feature flags link, so a feature administrator has no way to open the dialog",
				"apps/portal/web/src/app/components/shared/header/header.component.html: the header has no Feature flags link, so a feature administrator has no way to open the dialog",
			},
			wantDid: []string{
				"apps/console/web/src/app/components/shared/header/header.component.ts: the header injects MatDialog, declares flagsScope (List on FeatureFlags) and openFeatureFlags(), and imports the directive and the dialog",
				"apps/console/web/src/app/components/shared/header/header.component.html: the Feature flags button under *cccHasPermission=\"flagsScope\" before the username",
				"apps/portal/web/src/app/components/shared/header/header.component.ts: the header injects MatDialog, declares flagsScope (List on FeatureFlags) and openFeatureFlags(), and imports the directive and the dialog",
				"apps/portal/web/src/app/components/shared/header/header.component.html: the Feature flags button under *cccHasPermission=\"flagsScope\" before the username",
			},
			wantFiles: map[string]string{
				"apps/portal/web/src/app/components/shared/header/header.component.html": soloTemplateLinked,
			},
		},
		{
			name: "a header that opens the dialog is left alone",
			files: map[string]string{
				"web/console/src/app/components/shared/header/header.component.ts":   soloHeaderLinked,
				"web/console/src/app/components/shared/header/header.component.html": soloTemplateLinked,
			},
			wantFiles: map[string]string{
				"web/console/src/app/components/shared/header/header.component.ts": soloHeaderLinked,
			},
		},
		{
			name: "a reshaped template is left to the agent, with the class untouched",
			files: map[string]string{
				"web/console/src/app/components/shared/header/header.component.ts":   soloHeader,
				"web/console/src/app/components/shared/header/header.component.html": customTemplate,
			},
			wantDetect: []string{"web/console/src/app/components/shared/header/header.component.html: the header has no Feature flags link, so a feature administrator has no way to open the dialog"},
			wantSkip:   []string{"web/console/src/app/components/shared/header/header.component.html: the template has no username span the link goes before; put the Feature flags button under *cccHasPermission=\"flagsScope\" where the session controls are, and give the class flagsScope and openFeatureFlags() by hand"},
			wantFiles: map[string]string{
				"web/console/src/app/components/shared/header/header.component.ts":   soloHeader,
				"web/console/src/app/components/shared/header/header.component.html": customTemplate,
			},
		},
		{
			name: "a class without an injection line is left to the agent",
			files: map[string]string{
				"web/console/src/app/components/shared/header/header.component.ts":   strings.Replace(soloHeader, "  auth = inject(AuthService);\n", "  constructor(public auth: AuthService) {}\n", 1),
				"web/console/src/app/components/shared/header/header.component.html": soloTemplate,
			},
			wantDetect: []string{"web/console/src/app/components/shared/header/header.component.html: the header has no Feature flags link, so a feature administrator has no way to open the dialog"},
			wantSkip:   []string{"web/console/src/app/components/shared/header/header.component.ts: the component class injects nothing on a line of its own, where the dialog's injection goes; inject MatDialog, declare flagsScope (List on FeatureFlags) and openFeatureFlags() calling openFeatureFlagsDialog, and put the button under *cccHasPermission in the template by hand"},
			wantFiles: map[string]string{
				"web/console/src/app/components/shared/header/header.component.html": soloTemplate,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			writeTestApp(t, root)
			for rel, content := range tt.files {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o750); err != nil {
					t.Fatalf("os.MkdirAll() error = %v", err)
				}
				if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o600); err != nil {
					t.Fatalf("os.WriteFile() error = %v", err)
				}
			}
			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			r := FeatureFlagsLink{}
			found, err := r.Detect(t.Context(), a)
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantDetect, found); diff != "" {
				t.Errorf("Detect() mismatch (-want +got):\n%s", diff)
			}
			ch, err := r.Apply(t.Context(), a, nil)
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantDid, ch.Did); diff != "" {
				t.Errorf("Apply() Did mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantSkip, ch.Skipped); diff != "" {
				t.Errorf("Apply() Skipped mismatch (-want +got):\n%s", diff)
			}
			for rel, want := range tt.wantFiles {
				got, err := os.ReadFile(filepath.Join(root, rel))
				if err != nil {
					t.Fatalf("os.ReadFile(%s) error = %v", rel, err)
				}
				if diff := cmp.Diff(want, string(got)); diff != "" {
					t.Errorf("%s mismatch (-want +got):\n%s", rel, diff)
				}
			}
			again, err := r.Detect(t.Context(), a)
			if err != nil {
				t.Fatalf("Detect() after Apply() error = %v", err)
			}
			if len(tt.wantSkip) == 0 && len(again) != 0 {
				t.Errorf("Detect() after Apply() = %v, want nothing left", again)
			}
		})
	}
}

// writeTestApp writes the least an application needs for app.Discover: a module file and
// an angular.json in each web root the test's files imply.
func writeTestApp(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.25\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(go.mod) error = %v", err)
	}
	for _, web := range []string{"web", "apps/console/web", "apps/portal/web"} {
		if err := os.MkdirAll(filepath.Join(root, web), 0o750); err != nil {
			t.Fatalf("os.MkdirAll() error = %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, web, "angular.json"), []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("os.WriteFile(angular.json) error = %v", err)
		}
	}
}
