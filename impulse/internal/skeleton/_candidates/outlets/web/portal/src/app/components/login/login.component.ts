import { DOCUMENT } from '@angular/common';
import { Component, computed, inject } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { ActivatedRoute } from '@angular/router';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { API_URL, BASE_URL } from '@cccteam/resource-angular/types';
import { UiCoreService } from '@cccteam/resource-angular/ui-core-service';
import { IdleService } from '@cccteam/resource-angular/ui-idle-service';
import { map } from 'rxjs';

/**
 * The portal's people sign in through the organization's directory. The login page sends
 * the browser to the session's login route, which redirects it to the directory; the
 * directory returns it to the callback, which starts the session and returns the browser
 * to the page it was going to. A refused login comes back here with the reason as a code
 * in the query (?code=), never text; the page shows the sentence it holds for the code and
 * nothing for a code it does not know, so a crafted URL renders nothing.
 */
@Component({
  selector: 'app-login',
  templateUrl: './login.component.html',
  styleUrls: ['./login.component.scss'],
  imports: [MatButtonModule, MatCardModule],
})
export class LoginComponent {
  private auth = inject(AuthService);
  private ui = inject(UiCoreService);
  private idle = inject(IdleService);
  private apiUrl = inject(API_URL);
  private baseUrl = inject(BASE_URL);
  private document = inject(DOCUMENT);
  private route = inject(ActivatedRoute);

  /** The refusal code a refused login came back with, or empty: the only thing the URL says. */
  code = toSignal(this.route.queryParamMap.pipe(map((params) => params.get('code') ?? '')), {
    initialValue: '',
  });

  /** The sentence this page holds for the code, from the library's login messages; nothing for a code it does not know. */
  message = computed(() => this.ui.loginMessage(this.code()));

  constructor() {
    this.auth.logout().subscribe();
    this.idle.stop();
  }

  login(): void {
    const returnUrl = encodeURIComponent(this.getAndResetReturnUrl());
    this.document.location.assign(`${this.apiUrl}/user/login?returnUrl=${returnUrl}`);
  }

  /** The browser path the callback returns to: the page the guard turned away from, else the dashboard. */
  private getAndResetReturnUrl(): string {
    const redirectUrl = this.auth.redirectUrl();
    this.auth.redirectUrl.set('');
    if (redirectUrl === '' || redirectUrl.startsWith('/login')) {
      return `${this.baseUrl}dashboard`;
    }
    return `${this.baseUrl}${redirectUrl.replace(/^\//, '')}`;
  }
}
