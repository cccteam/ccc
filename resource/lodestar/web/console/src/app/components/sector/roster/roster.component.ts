import { Component, computed, inject } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatSelectModule } from '@angular/material/select';
import { MatTableModule } from '@angular/material/table';
import { Methods, Permissions, Resources } from '@app/service/zz_gen_constants';
import { CREW_MANIFEST } from '@components/login/login.component';
import { ImpersonationService } from '../impersonation.service';
import { SectorService } from '../sector.service';
import { StarChartComponent } from '../star-chart/star-chart.component';

/**
 * The squadron roster: the sector's wings and squadrons (the Wing Commander sees only
 * Forge Wing's, through subject.wings), the crew, and the two impersonation moments.
 * "View as" mints a read-only session as another crew member; "Act as role" mints a
 * session under a role with subject still bound to you. Both controls render only
 * when the digest carries the manual Execute registrations — checked by the generated
 * Methods constants, never a hand-typed string.
 */
@Component({
  selector: 'app-roster',
  imports: [FormsModule, MatButtonModule, MatCardModule, MatFormFieldModule, MatSelectModule, MatTableModule, StarChartComponent],
  templateUrl: './roster.component.html',
  styleUrl: './roster.component.scss',
})
export class RosterComponent {
  private sectors = inject(SectorService);
  impersonation = inject(ImpersonationService);

  wings = this.sectors.sectorList((sector) => sector.wings);
  squadrons = this.sectors.sectorList((sector) => sector.squadrons);
  memberships = this.sectors.sectorList((sector) => sector.squadronMemberships);
  pilots = this.sectors.globalList((api) => api.pilots);
  columns = ['name', 'wing', 'members'];

  sector = this.sectors.current;
  canListSquadrons = computed(() => this.sectors.can(Permissions.List, Resources.Squadrons));
  canViewAs = computed(() => this.sectors.can(Permissions.Execute, Methods.ViewAsUser));
  canAssumeRole = computed(() => this.sectors.can(Permissions.Execute, Methods.AssumeRole));
  alreadyImpersonating = computed(() => this.impersonation.record() !== undefined);

  readonly crew = CREW_MANIFEST;
  readonly roles = ['Dispatcher', 'Cadet', 'Pilot', 'Engineer', 'Archivist', 'BookingAgent', 'SectorMarshal'];

  viewAsUser = '';
  assumeRoleName = '';

  wingName(id: string | undefined): string {
    return this.wings.value().find((w) => w.id === id)?.name ?? '—';
  }

  members(squadronId: string): string {
    return this.memberships
      .value()
      .filter((m) => m.squadronId === squadronId)
      .map((m) => m.userId)
      .join(', ');
  }

  async viewAs(): Promise<void> {
    if (!this.viewAsUser) return;
    await this.impersonation.viewAs(this.viewAsUser);
  }

  async assumeRole(): Promise<void> {
    if (!this.assumeRoleName) return;
    await this.impersonation.assumeRole(this.assumeRoleName);
  }
}
