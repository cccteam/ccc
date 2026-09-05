import { DatePipe } from '@angular/common';
import { Component, computed, inject } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatTableModule } from '@angular/material/table';
import { Methods, Permissions, Resources } from '@app/service/zz_gen_constants';
import { Consignments } from '@app/service/zz_gen_resources';
import { SectorService } from '../sector.service';
import { StarChartComponent } from '../star-chart/star-chart.component';

/**
 * The salvage hold: cargo in bond until its owner claims it. Release is the plain
 * located-row form — the once-only rule rides the grant (releasedAt IS NULL), so the
 * Release button renders from the row's Execute answer and a released consignment
 * offers nothing. Deletes ride the date literal on the Supercargo's grant; the hold is
 * sorted by expiry server-side and Mass filters through allow_filter.
 */
@Component({
  selector: 'app-salvage-hold',
  imports: [DatePipe, FormsModule, MatButtonModule, MatCardModule, MatFormFieldModule, MatInputModule, MatTableModule, StarChartComponent],
  templateUrl: './salvage-hold.component.html',
  styleUrl: './salvage-hold.component.scss',
})
export class SalvageHoldComponent {
  private sectors = inject(SectorService);

  readonly methods = Methods;

  consignments = this.sectors.sectorList((sector) => sector.consignments, {
    sort: { field: 'expiresOn' },
    capabilities: ['Execute', 'Update', 'Delete'],
  });
  clients = this.sectors.globalList((api) => api.clients);
  columns = ['bondCode', 'description', 'client', 'mass', 'expiresOn', 'releasedAt', 'actions'];

  sector = this.sectors.current;
  canList = computed(() => this.sectors.can(Permissions.List, Resources.Consignments));

  minMass: number | null = null;

  // A typed filter over the allow_filter column, combined with the indexed bond code
  // prefix so the server's filter contract (one indexed field per group) holds.
  heavy = this.sectors.sectorList((sector) => sector.consignments, {
    filter: { and: [{ field: 'bondCode', op: 'isnotnull' }, { field: 'mass', op: 'gte', value: 100 }] },
  });

  clientName(id: string | undefined): string {
    return this.clients.value().find((c) => c.id === id)?.name ?? '—';
  }

  canRelease(row: Consignments): boolean {
    return this.sectors.sectorApi().consignments.rowCan(row, 'Execute', Methods.ReleaseConsignment);
  }

  canRemove(row: Consignments): boolean {
    return this.sectors.sectorApi().consignments.rowCan(row, 'Delete');
  }

  async release(row: Consignments): Promise<void> {
    await this.sectors.sectorApi().releaseConsignment.execute({ consignmentId: row.id });
    this.consignments.reload();
  }

  async remove(row: Consignments): Promise<void> {
    const handle = this.sectors.sectorApi().consignments;
    await handle.remove(handle.keyOf(row));
    this.consignments.reload();
    this.heavy.reload();
  }
}
