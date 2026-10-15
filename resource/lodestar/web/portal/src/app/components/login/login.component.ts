import { DOCUMENT, LowerCasePipe } from '@angular/common';
import { HttpClient } from '@angular/common/http';
import { Component, computed, inject, signal } from '@angular/core';
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

/**
 * One sign-in method the outlet offers, as the server's methods route describes it: the
 * registration's name, the library method behind it, how the page drives it, the label
 * to show, and the path the router mounted it at. The page names no path of its own.
 */
export interface LoginMethod {
  name: string;
  method: string;
  kind: 'password' | 'redirect';
  label: string;
  path: string;
}

/** What the methods route answers: the methods in the order they were registered, and the pending sign-in's routes. */
export interface LoginMethods {
  methods: LoginMethod[];
  pending?: { status: string; confirm: string; cancel: string };
}

/**
 * The portal's people sign in through their company's Google directory, whose groups
 * are their roles (RoleSync). The login page asks the outlet's methods route what the
 * members auth offers, the company directory alone, and sends the browser to the path the
 * route names for it; the directory returns the browser to the callback, which starts the
 * session and returns the browser to the page it was going to. In development the
 * session library's skipAuth build simulates the directory from APP_USERNAME and
 * APP_ROLES, so no Google tenant is needed. A refused login comes back here with the
 * reason as a code in the query (?code=), never text; the page shows the sentence it
 * holds for the code and nothing for a code it does not know, so a crafted URL renders
 * nothing.
 *
 * Demonstrates: auth.directory-roles, auth.skipauth-directory, auth.login-methods, login-manifest, auth.login-refusal-code.
 */
@Component({
  selector: 'app-login',
  templateUrl: './login.component.html',
  styleUrls: ['./login.component.scss'],
  imports: [LowerCasePipe, MatButtonModule, MatCardModule],
})
export class LoginComponent {
  private http = inject(HttpClient);
  private auth = inject(AuthService);
  private ui = inject(UiCoreService);
  private idle = inject(IdleService);
  private apiUrl = inject(API_URL);
  private baseUrl = inject(BASE_URL);
  private document = inject(DOCUMENT);
  private route = inject(ActivatedRoute);

  readonly manifest = CLIENT_MANIFEST;

  /** The methods the outlet offers, as the server listed them; empty until the route answers. */
  methods = signal<LoginMethod[]>([]);
  /** The redirect sign-ins, one button each, in the server's order: the company directory. */
  redirectMethods = computed(() => this.methods().filter((method) => method.kind === 'redirect'));

  /** The refusal code a refused login came back with, or empty: the only thing the URL says. */
  code = toSignal(this.route.queryParamMap.pipe(map((params) => params.get('code') ?? '')), {
    initialValue: '',
  });

  /** The sentence this page holds for the code, from the library's login messages; nothing for a code it does not know. */
  message = computed(() => this.ui.loginMessage(this.code()));

  constructor() {
    this.auth.logout().subscribe();
    this.idle.stop();
    this.http.get<LoginMethods>(`${this.apiUrl}/user/methods`).subscribe((list) => this.methods.set(list.methods));
  }

  /** Sends the browser to a redirect method's path, naming the page the callback returns to. */
  signInThrough(method: LoginMethod): void {
    const returnUrl = encodeURIComponent(this.getAndResetReturnUrl());
    this.document.location.assign(`${method.path}?returnUrl=${returnUrl}`);
  }

  /** The browser path the callback returns to: the page the guard turned away from, else the tracker. */
  private getAndResetReturnUrl(): string {
    const redirectUrl = this.auth.redirectUrl();
    this.auth.redirectUrl.set('');
    if (redirectUrl === '' || redirectUrl.startsWith('/login')) {
      return `${this.baseUrl}tracker`;
    }
    return `${this.baseUrl}${redirectUrl.replace(/^\//, '')}`;
  }
}
