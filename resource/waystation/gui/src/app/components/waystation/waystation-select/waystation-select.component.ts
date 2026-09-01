import { Component, inject } from '@angular/core';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatSelectModule } from '@angular/material/select';
import { MatSlideToggleModule } from '@angular/material/slide-toggle';
import { MatTooltipModule } from '@angular/material/tooltip';
import { WaystationService } from '../waystation.service';

/**
 * WaystationSelectComponent picks the active waystation — the permission domain
 * every station-scoped request runs in. The selection lives in WaystationService as a
 * signal, so it survives navigation, and the offered stations derive there from the
 * session permission map (or, with "Show all waystations" on, the generated
 * Waystations resource — the demo's clickable path to fail-closed refusals).
 */
@Component({
  selector: 'app-waystation-select',
  imports: [MatFormFieldModule, MatSelectModule, MatSlideToggleModule, MatTooltipModule],
  templateUrl: './waystation-select.component.html',
  styles: `
    .waystation-select {
      display: flex;
      align-items: center;
      gap: 16px;
    }

    /* Center the toggle on the input box, not on the form field's subscript area. */
    mat-slide-toggle {
      margin-bottom: 22px;
    }
  `,
})
export class WaystationSelectComponent {
  ws = inject(WaystationService);
}
