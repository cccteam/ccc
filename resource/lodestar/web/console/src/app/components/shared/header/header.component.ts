/** Demonstrates: impersonation.end, impersonation.max-duration, computed.user, idle.configured, @feature.field, @feature. */
import { Component, computed, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatDialog } from '@angular/material/dialog';
import { MatIconModule } from '@angular/material/icon';
import { Router, RouterModule } from '@angular/router';
import { Feature } from '@app/service/zz_gen_api';
import { Permissions, Resources } from '@app/service/zz_gen_constants';
import { FeatureDirective } from '@cccteam/resource-angular/auth-feature';
import { HasPermissionDirective } from '@cccteam/resource-angular/auth-has-permission';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { openFeatureFlagsDialog } from '@cccteam/resource-angular/ccc-feature-flags';
import { PermissionScope } from '@cccteam/resource-angular/types';
import { IdleService } from '@cccteam/resource-angular/ui-idle-service';
import { tap } from 'rxjs';
import { ImpersonationService } from '@components/sector/impersonation.service';
import { grantedColumns, SectorService } from '@components/sector/sector.service';
import { TopbarComponent } from '../topbar/topbar.component';

@Component({
  selector: 'app-header',
  imports: [MatButtonModule, MatIconModule, RouterModule, TopbarComponent, FeatureDirective, HasPermissionDirective],
  templateUrl: './header.component.html',
  styleUrls: ['./header.component.scss'],
})
export class HeaderComponent {
  private router = inject(Router);
  private dialog = inject(MatDialog);
  idle = inject(IdleService);
  auth = inject(AuthService);
  impersonation = inject(ImpersonationService);
  private sectors = inject(SectorService);
  // The pilot card is the caller-scoped read: a computed resource whose List yields
  // the one row that is "mine", chosen server-side by the identity the permission
  // check ran as — so under a view-as session the header shows the viewed person's
  // standing, not the actor's. The card asks for the columns the digest grants
  // (grantedColumns): its citation count is a field behind the commendations flag
  // (@feature.field), which the server serves only when it is named and the digest
  // names exactly while the flag is on, so the read follows the flag, and the person
  // who flips it sees their own card re-read.
  cards = this.sectors.globalList((api) => api.pilotCards, grantedColumns);
  card = computed(() => this.cards.value()[0]);

  // The banner reads the session's impersonation record: "Viewing as Cadet Cass,
  // read-only. You are Maren Voss." — present exactly when the session was minted
  // through the impersonation route.
  banner = computed(() => this.impersonation.banner());

  /**
   * The flag the card's citation count sits behind (@feature.field on PilotCards): the
   * generated member, never the string, so a misspelled flag fails to compile. The
   * enabled set and the digest the card's columns follow are loaded together at sign-in
   * and refreshed together by a flip, so the count is in the row whenever the flag says
   * to show it.
   */
  readonly commendations = Feature.Commendations;

  /**
   * The grant that shows the Feature flags link: List on the flags, which is what the
   * library's dialog reads. Whether the dialog may flip one is the digest's Execute on
   * SetFeature, which the dialog asks for itself; with List alone it is read-only.
   */
  readonly flagsScope: PermissionScope = { resource: Resources.FeatureFlags, permission: Permissions.List };

  /** Opens the library's feature flags dialog over the console. */
  openFeatureFlags(): void {
    openFeatureFlagsDialog(this.dialog);
  }

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
