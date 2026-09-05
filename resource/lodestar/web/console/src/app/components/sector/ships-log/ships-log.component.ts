import { DatePipe } from '@angular/common';
import { Component, computed, inject, resource } from '@angular/core';
import { MatCardModule } from '@angular/material/card';
import { MatTableModule } from '@angular/material/table';
import { Permissions, Resources } from '@app/service/zz_gen_constants';
import { SectorService, ShipsLogEntry } from '../sector.service';
import { StarChartComponent } from '../star-chart/star-chart.component';

/**
 * The ship's log is change tracking rendered as a log: who claimed, who reassigned,
 * who hailed. It is the manual-resource demo — DataChangeEvents is library
 * infrastructure with no generated handler, so the surface is a hand-written,
 * sector-scoped route whose List permission was registered through
 * @manualAddResource(List, domain). A Hail shows up as an entry with every field
 * unchanged but the timestamp — that is what a touch is. Under an assumed role the
 * source reads "greer as role Dispatcher": the actor-aware change event.
 */
@Component({
  selector: 'app-ships-log',
  imports: [DatePipe, MatCardModule, MatTableModule, StarChartComponent],
  templateUrl: './ships-log.component.html',
  styleUrl: './ships-log.component.scss',
})
export class ShipsLogComponent {
  private sectors = inject(SectorService);

  sector = this.sectors.current;
  canList = computed(() => this.sectors.can(Permissions.List, Resources.ShipsLogEntries));
  columns = ['eventTime', 'tableName', 'rowId', 'eventSource', 'changeSet'];

  entries = resource({
    params: () => ({ sector: this.sectors.current(), allowed: this.canList() }),
    loader: ({ params }) => (params.sector && params.allowed ? this.sectors.shipsLog(params.sector).list() : Promise.resolve([])),
    defaultValue: [] as ShipsLogEntry[],
  });

  changeSetLabel(entry: ShipsLogEntry): string {
    return entry.changeSet ? JSON.stringify(entry.changeSet) : '—';
  }

  isTouch(entry: ShipsLogEntry): boolean {
    const keys = entry.changeSet ? Object.keys(entry.changeSet) : [];
    return keys.length === 1 && keys[0] === 'UpdatedAt';
  }
}
