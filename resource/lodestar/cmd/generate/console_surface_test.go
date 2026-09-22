package generate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestConsoleCarriesLibrarySurface pins the browser-library surface the console carries
// beyond its generated pages, which no other application exercises since ccc-lib's
// showcase retired: the idle session provided from the build's environment through the
// library's six tokens, with confirmation required and the stay-logged-in action in the
// header; the Squadrons page's componentConfig (the channel card) and arrayConfig (the
// sector's other squadrons, filtered by the indexed key); and the leave-page
// confirmation, which the library's resourceRoutes attaches to every config-driven list
// and row route under the FRONTEND_LOGIN_PATH the console provides, so the console adds
// no guard of its own. Lodestar has no browser specs and the walkthrough is curl, so the
// committed sources are the proof the wiring is present; the dialogs are driven by hand
// in the browser (README, "Running it"). Needs no emulator.
//
// Demonstrates: idle.configured, config.component, config.array, form.leave-page.
func TestConsoleCarriesLibrarySurface(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	console := filepath.Join(filepath.Dir(thisFile), "..", "..", "web", "console", "src")

	tests := []struct {
		name   string
		file   string
		want   []string
		absent []string
	}{
		{
			name: "the six idle tokens are provided, the durations from the environment and confirmation required",
			file: "app/app.config.ts",
			want: []string{
				"{ provide: IDLE_SESSION_DURATION, useValue: environment.idle.sessionSeconds }",
				"{ provide: IDLE_WARNING_DURATION, useValue: environment.idle.warningSeconds }",
				"{ provide: IDLE_KEEPALIVE_DURATION, useValue: environment.idle.keepAliveSeconds }",
				"{ provide: IDLE_TIMEOUT_REQUIRE_CONFIRMATION, useValue: true }",
				"provide: IDLE_LOGOUT_ACTION,",
				"provide: LOGOUT_ACTION,",
				"{ provide: FRONTEND_LOGIN_PATH, useValue: '/login' }",
			},
		},
		{
			name: "a development build shortens the idle session",
			file: "environments/environment.ts",
			want: []string{"idle: { sessionSeconds: 180, warningSeconds: 60, keepAliveSeconds: 30 }"},
		},
		{
			name: "the served build's idle session matches the server's default session timeout",
			file: "environments/environment.prod.ts",
			want: []string{"idle: { sessionSeconds: 600, warningSeconds: 60, keepAliveSeconds: 30 }"},
		},
		{
			name: "the header renders the stay-logged-in action while the warning holds",
			file: "app/components/shared/header/header.component.html",
			want: []string{"@if (idle.isWarning())", `(click)="idle.stayLoggedIn()"`, "idle.countdown()"},
		},
		{
			name: "the Squadrons page carries the channel card and the sector's other squadrons",
			file: "app/configs/squadrons.config.ts",
			want: []string{
				"componentConfig({ primaryResource: Resources.Squadrons, component: SquadronChannelComponent })",
				"arrayConfig({",
				"title: 'Other squadrons in this sector'",
				"`${Squadrons.fieldName.id}:ne:${squadron.id}`",
			},
		},
		{
			name: "the channel card is the application's component over the library's base class",
			file: "app/components/sector/squadron-channel/squadron-channel.component.ts",
			want: []string{"export class SquadronChannelComponent extends CustomConfigComponent"},
		},
		{
			name:   "the config-driven routes come from resourceRoutes, which carries the leave-page guard, and the console adds none",
			file:   "app/app.routes.ts",
			want:   []string{"resourceRoutes(squadronsConfig, resourceMeta)", "resourceRoutes(missionsConfig, resourceMeta)"},
			absent: []string{"canDeactivate: ["},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(filepath.Join(console, tt.file))
			if err != nil {
				t.Fatalf("reading %s: %v", tt.file, err)
			}
			source := string(data)
			for _, want := range tt.want {
				if !strings.Contains(source, want) {
					t.Errorf("%s: missing %q", tt.file, want)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(source, absent) {
					t.Errorf("%s: carries %q, want absent", tt.file, absent)
				}
			}
		})
	}
}

// TestConsoleHandsTheClientItsHooks pins how both browser applications render the
// client's judgment, where the library's HTTP interceptor used to judge responses one
// layer below the client and toast every in-place refusal a second time: each
// app.config.ts spreads the options provideResourceClient hands its factory (the
// transport that counts activity, the error hook that returns the browser to the login
// page on a 401) into the generated createApi and registers no interceptor, HttpClient
// keeps the XSRF echo alone, no provider after it re-provides ErrorHandler (the
// BrowserAnimationsModule import that did is the standalone provideAnimationsAsync), each
// login page reads AuthService.redirectUrl once and clears it, and the flight deck reports its refusals in place (the declared 409 answer,
// the 403 on an edit) so they reach no notice. Lodestar has no browser specs, so the
// committed sources are the proof the wiring is present; the redirect and the notice are
// driven in the browser (README, "Running it"). Needs no emulator.
//
// Demonstrates: client.login-redirect, client.uncaught-notice.
func TestConsoleHandsTheClientItsHooks(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	web := filepath.Join(filepath.Dir(thisFile), "..", "..", "web")

	// No interceptor, and no importProvidersFrom(BrowserAnimationsModule): BrowserModule's providers
	// carry Angular's default ErrorHandler, which would replace the adapter's and silence the notice.
	noInterceptor := []string{"HTTP_INTERCEPTORS", "ui-interceptor", "withInterceptorsFromDi", "ApiInterceptor", "BrowserAnimationsModule", "importProvidersFrom"}

	tests := []struct {
		name   string
		file   string
		want   []string
		absent []string
	}{
		{
			name: "the console spreads the adapter's options into createApi and registers no interceptor",
			file: "console/src/app/app.config.ts",
			want: []string{
				"provideResourceClient((options) => createApi({ baseUrl: environment.apiUrl, ...options }))",
				"provideHttpClient(withXsrfConfiguration({ cookieName: 'crew-xsrf' }))",
				"provideAnimationsAsync(),",
				"{ provide: FRONTEND_LOGIN_PATH, useValue: '/login' }",
				"{ provide: BASE_URL, useValue: environment.baseUrl }",
			},
			absent: noInterceptor,
		},
		{
			name: "the portal the same, over its own cookie",
			file: "portal/src/app/app.config.ts",
			want: []string{
				"provideResourceClient((options) => createApi({ baseUrl: environment.apiUrl, ...options }))",
				"provideHttpClient(withXsrfConfiguration({ cookieName: 'members-xsrf' }))",
				"provideAnimationsAsync(),",
			},
			absent: noInterceptor,
		},
		{
			name: "the console's login page returns to the URL the hook kept, read once and cleared",
			file: "console/src/app/components/login/login.component.ts",
			want: []string{"const redirectUrl = this.auth.redirectUrl();", "this.auth.redirectUrl.set('');"},
		},
		{
			name: "the portal's login page the same, through the directory's return URL",
			file: "portal/src/app/components/login/login.component.ts",
			want: []string{"const redirectUrl = this.auth.redirectUrl();", "this.auth.redirectUrl.set('');"},
		},
		{
			name: "the flight deck reports the declared answer and the edit refusal in place",
			file: "console/src/app/components/sector/flight-deck/flight-deck.component.ts",
			want: []string{
				"if (answer.status === 409) {",
				"this.refusal.set(",
				"if (e instanceof ApiError && e.status === 403) {",
				"this.editRefusal.set(e.message);",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(filepath.Join(web, tt.file))
			if err != nil {
				t.Fatalf("reading %s: %v", tt.file, err)
			}
			source := string(data)
			for _, want := range tt.want {
				if !strings.Contains(source, want) {
					t.Errorf("%s: missing %q", tt.file, want)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(source, absent) {
					t.Errorf("%s: carries %q, want absent", tt.file, absent)
				}
			}
		})
	}
}
