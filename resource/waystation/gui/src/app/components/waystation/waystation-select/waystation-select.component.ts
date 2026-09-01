import { Component, inject, OnInit } from '@angular/core';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatSelectModule } from '@angular/material/select';
import { WaystationService } from '../waystation.service';

/**
 * WaystationSelectComponent picks the active waystation — the permission domain
 * every station-scoped request runs in. The selection lives in WaystationService as a
 * signal, so it survives navigation and the station pages reload through an effect
 * whenever it changes.
 */
@Component({
  selector: 'app-waystation-select',
  imports: [MatFormFieldModule, MatSelectModule],
  templateUrl: './waystation-select.component.html',
})
export class WaystationSelectComponent implements OnInit {
  ws = inject(WaystationService);

  ngOnInit(): void {
    this.ws.loadDirectory();
  }
}
