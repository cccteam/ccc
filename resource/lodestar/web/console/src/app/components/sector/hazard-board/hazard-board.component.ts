import { DatePipe } from '@angular/common';
import { Component, computed, inject } from '@angular/core';
import { MatCardModule } from '@angular/material/card';
import { MatTableModule } from '@angular/material/table';
import { Permissions, Resources } from '@app/service/zz_gen_constants';
import { SectorService } from '../sector.service';
import { StarChartComponent } from '../star-chart/star-chart.component';

/**
 * The hazard board is the only human window onto droid telemetry: the worst reading
 * per ship and subsystem, computed server-side from DroidReports that have no browser
 * route at all. The Hazard Analyst's List grant carries a row-free expiry, so the
 * board itself goes dark when the certification lapses.
 */
@Component({
  selector: 'app-hazard-board',
  imports: [DatePipe, MatCardModule, MatTableModule, StarChartComponent],
  templateUrl: './hazard-board.component.html',
  styleUrl: './hazard-board.component.scss',
})
export class HazardBoardComponent {
  private sectors = inject(SectorService);

  rows = this.sectors.sectorList((sector) => sector.sectorHazardBoards);
  columns = ['shipName', 'subsystem', 'worstReading', 'recordedAt'];

  sector = this.sectors.current;
  canList = computed(() => this.sectors.can(Permissions.List, Resources.SectorHazardBoards));

  severity(reading: number): string {
    return reading >= 0.8 ? 'critical' : reading >= 0.5 ? 'elevated' : 'nominal';
  }
}
