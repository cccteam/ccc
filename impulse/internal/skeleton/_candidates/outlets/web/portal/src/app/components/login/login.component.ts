import { Component, inject } from '@angular/core';
import { MatCardModule } from '@angular/material/card';
import { Router } from '@angular/router';
import { LoginMethodsComponent } from '@cccteam/resource-angular/auth-login-methods';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { IdleService } from '@cccteam/resource-angular/ui-idle-service';

/** The portal's own words for the directory method, keyed by its registration name; the server's label is the default. */
export const METHOD_LABELS: Readonly<Record<string, string>> = { directory: 'your organization' };

/**
 * The portal's people sign in through the organization's directory. The page composes the
 * library's login component, which asks the outlet's methods route what the members auth
 * offers, the directory alone, and sends the browser to the path the route names for it,
 * with the page to return to; the directory returns the browser to the callback, which
 * starts the session and returns the browser to the page it was going to. The page's own
 * words for the button come as a label override keyed by the registration's name. A
 * refused login comes back here with the reason as a code in the query (?code=), never
 * text; the component shows the sentence the library holds for the code and nothing for a
 * code it does not know, so a crafted URL renders nothing.
 */
@Component({
  selector: 'app-login',
  templateUrl: './login.component.html',
  styleUrls: ['./login.component.scss'],
  imports: [LoginMethodsComponent, MatCardModule],
})
export class LoginComponent {
  private auth = inject(AuthService);
  private router = inject(Router);
  private idle = inject(IdleService);

  readonly labels = METHOD_LABELS;

  /** The page ends whatever session the browser arrived with; the component's first request, the methods list, follows. */
  constructor() {
    this.auth.logout().subscribe();
    this.idle.stop();
  }

  /** Signed in and the session checked: the idle watch starts and the browser moves to the page the guard turned away from, else the dashboard. */
  go(page: string): void {
    this.idle.start();
    void this.router.navigateByUrl(page);
  }
}
