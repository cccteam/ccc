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

/** The portal's manifest lists client logins only — the same quick-fill helper, one card. */
export const CLIENT_MANIFEST = [
  { login: 'client', name: 'Client Cleo', company: 'Halvard Freight', proves: 'Tracks her company’s missions, stands one down, files distress calls.' },
];

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
  readonly manifest = CLIENT_MANIFEST;

  fillPersona(login: string): void {
    this.username = login;
    this.password = 'lodestar';
  }

  constructor() {
    this.auth.logout().subscribe();
    this.idle.stop();
  }

  login(): void {
    if (this.busy()) return;
    this.busy.set(true);
    this.http.post(`${this.apiUrl}/user/login`, { username: this.username, password: this.password }).subscribe({
      next: () => {
        this.auth.checkUserSession().subscribe({
          next: () => {
            this.idle.start();
            this.router.navigateByUrl('/');
          },
          complete: () => this.busy.set(false),
        });
      },
      error: () => this.busy.set(false),
    });
  }
}
