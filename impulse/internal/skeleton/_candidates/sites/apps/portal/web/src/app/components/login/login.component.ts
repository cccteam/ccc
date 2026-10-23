import { Component, inject } from '@angular/core';
import { MatCardModule } from '@angular/material/card';
import { Router } from '@angular/router';
import { LoginMethodsComponent } from '@cccteam/resource-angular/auth-login-methods';
import { IdleService } from '@cccteam/resource-angular/ui-idle-service';
import { ReleaseComponent } from '@cccteam/resource-angular/ui-release';

/**
 * The login page composes the library's login component. The component asks the outlet's
 * methods route what the auth offers, in the order the generator program registered the
 * methods, and renders it: the password form for the password method, posted to the path
 * the route names after the list is read again (the read refreshes the XSRF cookie the
 * post needs), and a button for each directory method, a redirect it sends the browser to
 * with the page to return to. A refused or waiting sign-in comes back here with the reason
 * as a code in the query (?code=) or the step in ?pending=, never text; the component
 * shows the sentence the library holds for the code, nothing for a code it does not know,
 * and the confirmation form for a step that waits. The page keeps what is its own: the
 * branding, the development notes, and where a sign-in lands.
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

  /**
   * The page ends no session: the component's first request, the methods list, primes the
   * XSRF cookie the sign-in's post needs, and a session the browser arrives with is the
   * component's to offer Continue or Sign out, or to accept a mailed link with. Signing out
   * is the sign-out route's.
   */
  constructor() {
    this.idle.stop();
  }

  /** Signed in and the session checked: the idle watch starts and the browser moves to the page the guard turned away from, else the dashboard. */
  go(page: string): void {
    this.idle.start();
    void this.router.navigateByUrl(page);
  }
}
