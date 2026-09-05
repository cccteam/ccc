import { HttpClient } from '@angular/common/http';
import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { Router } from '@angular/router';
import { AuthService } from '@cccteam/ccc-lib/auth-service';
import { API_URL } from '@cccteam/ccc-lib/types';
import { IdleService } from '@cccteam/ccc-lib/ui-idle-service';

/**
 * The login page posts the credentials to the session's login route, then loads the
 * session (and with it the permission digest) before entering the application.
 */
@Component({
  selector: 'app-login',
  templateUrl: './login.component.html',
  styleUrls: ['./login.component.scss'],
  imports: [FormsModule, MatButtonModule, MatCardModule, MatFormFieldModule, MatInputModule],
})
export class LoginComponent {
  private http = inject(HttpClient);
  private auth = inject(AuthService);
  private router = inject(Router);
  private idle = inject(IdleService);
  private apiUrl = inject(API_URL);

  username = '';
  password = '';
  busy = signal(false);
  failed = signal(false);

  constructor() {
    this.auth.logout().subscribe();
    this.idle.stop();
  }

  login(): void {
    if (this.busy()) {
      return;
    }
    this.busy.set(true);
    this.failed.set(false);

    this.http.post(`${this.apiUrl}/user/login`, { username: this.username, password: this.password }).subscribe({
      next: () => {
        this.auth.checkUserSession().subscribe({
          next: () => {
            this.idle.start();
            this.router.navigateByUrl(this.getAndResetRedirectUrl());
          },
          complete: () => this.busy.set(false),
        });
      },
      error: () => {
        this.failed.set(true);
        this.busy.set(false);
      },
    });
  }

  private getAndResetRedirectUrl(): string {
    const redirectUrl = this.auth.redirectUrl();
    this.auth.redirectUrl.set('');
    if (redirectUrl === '' || redirectUrl.startsWith('/login')) {
      return '/dashboard';
    }
    return redirectUrl;
  }
}
