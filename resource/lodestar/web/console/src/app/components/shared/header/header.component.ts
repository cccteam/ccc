import { Component, computed, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { Router, RouterModule } from '@angular/router';
import { AuthService } from '@cccteam/ccc-lib/auth-service';
import { IdleService } from '@cccteam/ccc-lib/ui-idle-service';
import { tap } from 'rxjs';
import { ImpersonationService } from '@components/sector/impersonation.service';
import { TopbarComponent } from '../topbar/topbar.component';

@Component({
  selector: 'app-header',
  imports: [MatButtonModule, MatIconModule, RouterModule, TopbarComponent],
  templateUrl: './header.component.html',
  styleUrls: ['./header.component.scss'],
})
export class HeaderComponent {
  private router = inject(Router);
  private idle = inject(IdleService);
  auth = inject(AuthService);
  impersonation = inject(ImpersonationService);

  // The banner reads the session's impersonation record: "Viewing as Cadet Cass,
  // read-only. You are Maren Voss." — present exactly when the session was minted
  // through the impersonation route.
  banner = computed(() => this.impersonation.banner());

  logout(): void {
    this.idle.stop();
    this.auth
      .logout()
      .pipe(tap(() => this.router.navigate(['/login'])))
      .subscribe();
  }

  /**
   * Return to yourself: end the minted session and land back in the actor's own
   * session. When that session is gone (expired, or past the hard cap) there is
   * nothing to return to, so the actor logs in again.
   */
  async endImpersonation(): Promise<void> {
    const restored = await this.impersonation.end();
    if (!restored) {
      this.logout();
    }
  }
}
