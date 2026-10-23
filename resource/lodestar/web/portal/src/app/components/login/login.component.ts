import { Component, inject } from '@angular/core';
import { MatCardModule } from '@angular/material/card';
import { Router } from '@angular/router';
import { LoginMethodsComponent } from '@cccteam/resource-angular/auth-login-methods';
import { IdleService } from '@cccteam/resource-angular/ui-idle-service';
import { ReleaseComponent } from '@cccteam/resource-angular/ui-release';

/**
 * The portal's manifest lists the client logins the simulated directory can present: one
 * card, since APP_USERNAME names the login and APP_ROLES its groups.
 */
export const CLIENT_MANIFEST = [
  {
    login: 'client',
    name: 'Client Cleo',
    company: 'Halvard Freight',
    proves: 'Tracks her company’s missions page by page, stands one down, files distress calls, reads the statement.',
  },
];

/** The portal's own words for the directory method, keyed by its registration name; the server's label is the default. */
export const METHOD_LABELS: Readonly<Record<string, string>> = { directory: 'your company directory' };

/**
 * The portal's people sign in through their company's Google directory, whose groups
 * are their roles (RoleSync). The page composes the library's login component, which asks
 * the outlet's methods route what the members auth offers, the company directory alone,
 * and sends the browser to the path the route names for it, with the page to return to;
 * the directory returns the browser to the callback, which starts the session and returns
 * the browser to the page it was going to. The page's own words for the button come as a
 * label override keyed by the registration's name. In development the session library's
 * skipAuth build simulates the directory from APP_USERNAME and APP_ROLES, so no Google
 * tenant is needed. A refused login comes back here with the reason as a code in the query
 * (?code=), never text; the component shows the sentence the library holds for the code and
 * nothing for a code it does not know, so a crafted URL renders nothing.
 *
 * Demonstrates: auth.directory-roles, auth.skipauth-directory, auth.login-methods, login-manifest, auth.login-refusal-code, client.login-component.
 */
@Component({
  selector: 'app-login',
  templateUrl: './login.component.html',
  styleUrls: ['./login.component.scss'],
  imports: [LoginMethodsComponent, MatCardModule, ReleaseComponent],
})
export class LoginComponent {
  private router = inject(Router);
  private idle = inject(IdleService);

  readonly manifest = CLIENT_MANIFEST;
  readonly labels = METHOD_LABELS;

  /**
   * The page ends no session: the component's first request, the methods list, primes the
   * XSRF cookie the sign-in's post needs, and a session the browser arrives with is the
   * component's to offer Continue or Sign out, or to accept a mailed link with. Signing out
   * is the sign-out route's.
   */
  constructor() {
    this.idle.stop();
  }

  /** Signed in and the session checked: the idle watch starts and the browser moves to the page the guard turned away from, else the tracker. */
  go(page: string): void {
    this.idle.start();
    void this.router.navigateByUrl(page);
  }
}
