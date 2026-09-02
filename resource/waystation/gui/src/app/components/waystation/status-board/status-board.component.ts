import { Component, computed, inject } from '@angular/core';
import { DatePipe } from '@angular/common';
import { MatCardModule } from '@angular/material/card';
import { MatTableModule } from '@angular/material/table';
import { Permissions, Resources } from '@app/service/zz_gen_constants';
import { StationStatusBoards } from '@app/service/zz_gen_resources';
import { WaystationService } from '../waystation.service';
import { WaystationSelectComponent } from '../waystation-select/waystation-select.component';

/**
 * The status board is a computed resource: its rows are folded server-side from the
 * latest sensor reading per facility and metric. The readings themselves arrive only
 * through the automation outlet (an API-keyed service account) and have no
 * human-readable route at all — this board is the only window onto them.
 */
@Component({
  selector: 'app-status-board',
  imports: [DatePipe, MatCardModule, MatTableModule, WaystationSelectComponent],
  templateUrl: './status-board.component.html',
  styleUrl: './status-board.component.scss',
})
export class StatusBoardComponent {
  private ws = inject(WaystationService);

  rows = this.ws.stationList<StationStatusBoards>('station-status-boards', Resources.StationStatusBoards);
  columns = ['facilityName', 'metric', 'latestReading', 'recordedAt'];

  station = this.ws.current;
  canList = computed(() => this.ws.can(Permissions.List, Resources.StationStatusBoards));
}
