import { Component, computed, inject } from '@angular/core';
import { MatCardModule } from '@angular/material/card';
import { MatIconModule } from '@angular/material/icon';
import { MatTableModule } from '@angular/material/table';
import { RouterModule } from '@angular/router';
import { Methods, Permissions, Resources } from '@app/service/zz_gen_constants';
import { ResourceScopes } from '@app/service/zz_gen_resources';
import { AuthService } from '@cccteam/ccc-lib/auth-service';
import { Method, Permission, Resource } from '@cccteam/resource';
import { ImpersonationService } from '@components/sector/impersonation.service';
import { SectorService } from '@components/sector/sector.service';
import { StarChartComponent } from '@components/sector/star-chart/star-chart.component';

/** One row of the service card: a resource and what the session holds on it here. */
interface CardRow {
  target: Resource | Method;
  label: string;
  scope: 'global' | 'domain';
  states: Record<string, 'granted' | 'conditional' | undefined>;
}

const READ_WRITE: Permission[] = [Permissions.List, Permissions.Read, Permissions.Create, Permissions.Update, Permissions.Delete];

/**
 * The dashboard is the service card: a badge listing, per resource, what you hold in
 * the selected sector — granted, conditional ("terms apply"), or absent. It is the
 * permission digest rendered as an object in the world; every deck consults the same
 * digest before issuing a request. Under a masked "view as" session the write column
 * carries a stripe: Masked drops those permissions from the digest itself.
 */
@Component({
  selector: 'app-dashboard',
  imports: [MatCardModule, MatIconModule, MatTableModule, RouterModule, StarChartComponent],
  templateUrl: './dashboard.component.html',
  styleUrl: './dashboard.component.scss',
})
export class DashboardComponent {
  auth = inject(AuthService);
  sectors = inject(SectorService);
  impersonation = inject(ImpersonationService);

  readonly permissions = READ_WRITE;
  readonly columns = ['label', ...READ_WRITE.map((p) => String(p)), 'execute'];

  // ServiceLedgers is a computed resource aggregated across every sector — the
  // Governor's view. The list gate is the handle's own.
  ledger = this.sectors.globalList((api) => api.serviceLedgers);
  ledgerColumns = ['name', 'openMissions', 'feesOutstanding', 'settlements'];

  sector = this.sectors.current;

  // The card's rows: every resource the descriptor knows, sector-scoped ones asked in
  // the selected sector. Absent everywhere means the row still renders, greyed — the
  // badge shows what you do NOT hold as plainly as what you do.
  rows = computed<CardRow[]>(() => {
    const resources = Object.values(Resources) as Resource[];
    return resources
      .map((res) => ({
        target: res,
        label: String(res),
        scope: (ResourceScopes[res] ?? 'global') as 'global' | 'domain',
        states: Object.fromEntries(READ_WRITE.map((p) => [String(p), this.sectors.state(p, res)])),
      }))
      .filter((row) => row.scope === 'domain' || Object.values(row.states).some((s) => s !== undefined));
  });

  // Execute grants: the methods lit in the selected sector (transitions and plain
  // targets) plus the global ones.
  methods = computed(() =>
    (Object.values(Methods) as Method[])
      .map((m) => ({ method: m, state: this.sectors.state(Permissions.Execute, m) }))
      .filter((m) => m.state !== undefined),
  );

  masked(permission: string): boolean {
    return this.impersonation.masked(permission);
  }

  emptyMessage = computed(() => {
    const sector = this.sectors.current();
    return sector ? `Your posting in ${sector} does not cover this deck.` : 'No sector is lit on your chart.';
  });
}
