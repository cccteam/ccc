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
}
