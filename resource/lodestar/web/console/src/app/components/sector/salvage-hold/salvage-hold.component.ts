import { DatePipe } from '@angular/common';
import { Component, computed, inject, signal } from '@angular/core';
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
 *
 * Demonstrates: paging.nullable-sort, paging.descriptor-sizes, allow_filter, @attribute.date, execute-condition, rpc.typed-result.
 */
@Component({
  selector: 'app-salvage-hold',
  imports: [DatePipe, FormsModule, MatButtonModule, MatCardModule, MatFormFieldModule, MatInputModule, MatTableModule, StarChartComponent],
  templateUrl: './salvage-hold.component.html',
  styleUrl: './salvage-hold.component.scss',
})
export class SalvageHoldComponent {
  sectors = inject(SectorService);

  readonly methods = Methods;

  // The hold walks its declared order, releasedAt desc, a NULLABLE sort column: cargo
  // still in bond (NULL) comes first descending and last ascending, and the cursor
  // crosses the boundary in both directions without a repeat or a skip. The page size is
  // the descriptor's.
  readonly pageSize = this.sectors.pageSize(Resources.Consignments);
  direction = signal<'asc' | 'desc'>('desc');
  consignmentsPage = this.sectors.sectorPage((sector) => sector.consignments, {
    limit: this.pageSize,
    count: true,
    capabilities: ['Execute', 'Update', 'Delete'],
  });
  consignments = computed(() => this.consignmentsPage.value()?.rows ?? []);
  total = computed(() => this.consignmentsPage.value()?.total);
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

  /** Steps to the neighboring page the server named; the total from the first page stays shown. */
  async turnPage(direction: 'next' | 'prev'): Promise<void> {
    const page = this.consignmentsPage.value();
    const step = direction === 'next' ? page?.next : page?.prev;
    if (!step) return;
    const total = page?.total;
    const turned = await step();
    this.consignmentsPage.set({ ...turned, total: turned.total ?? total });
  }

  /** Flips the walk: ascending puts unreleased cargo last, the NULL region at the other end. */
  async flip(): Promise<void> {
    this.direction.set(this.direction() === 'desc' ? 'asc' : 'desc');
    const handle = this.sectors.sectorApi().consignments;
    this.consignmentsPage.set(
      await handle.page({
        sort: { field: 'releasedAt', direction: this.direction() },
        limit: this.pageSize,
        count: true,
        capabilities: ['Execute', 'Update', 'Delete'],
      }),
    );
  }

  /** The receipt of the last release: the typed answer the method returns. */
  receipt = signal<string | undefined>(undefined);

  async release(row: Consignments): Promise<void> {
    const released = await this.sectors.sectorApi().releaseConsignment.execute({ consignmentId: row.id });
    this.receipt.set(`${released.bondCode} released at ${released.releasedAt}`);
    this.consignmentsPage.reload();
  }

  async remove(row: Consignments): Promise<void> {
    const handle = this.sectors.sectorApi().consignments;
    await handle.remove(handle.keyOf(row));
    this.consignmentsPage.reload();
    this.heavy.reload();
  }
}
