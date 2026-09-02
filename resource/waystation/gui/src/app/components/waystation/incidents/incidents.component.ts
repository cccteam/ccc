import { Component, computed, inject } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatTableModule } from '@angular/material/table';
import { Permissions, Resources } from '@app/service/zz_gen_constants';
import { IncidentReports } from '@app/service/zz_gen_resources';
import { WaystationService } from '../waystation.service';
import { WaystationSelectComponent } from '../waystation-select/waystation-select.component';

/**
 * Incident reports demonstrate the server-owned field shapes in one form: the case
 * number is output-only (defaulted server-side, unwritable from the wire), the
 * reporter contact is PII the auditor role reads around (the column is simply absent
 * from their rows), and severity is clamped by an update-defaults hook.
 */
@Component({
  selector: 'app-incidents',
  imports: [
    FormsModule,
    MatButtonModule,
    MatCardModule,
    MatFormFieldModule,
    MatInputModule,
    MatTableModule,
    WaystationSelectComponent,
  ],
  templateUrl: './incidents.component.html',
  styleUrl: './incidents.component.scss',
})
export class IncidentsComponent {
  private ws = inject(WaystationService);

  incidents = this.ws.stationList<IncidentReports>('incident-reports', Resources.IncidentReports);
  columns = ['caseNumber', 'summary', 'severity', 'reporterContact'];

  // Affordances from the selected station's digest.
  station = this.ws.current;
  canList = computed(() => this.ws.can(Permissions.List, Resources.IncidentReports));
  canReport = computed(() => this.ws.can(Permissions.Create, Resources.IncidentReports));

  newSummary = '';
  newSeverity: number | null = null;
  newReporterContact = '';
  newRawStatement = '';

  // contactLabel renders the PII column: an absent key means this persona's grant
  // excludes the field — render it withheld, never blank-as-if-empty.
  contactLabel(incident: IncidentReports): string {
    return 'reporterContact' in incident ? (incident.reporterContact ?? '') : '— withheld —';
  }

  report(): void {
    if (!this.newSummary || this.newSeverity === null) {
      return;
    }
    this.ws
      .createIncident({
        summary: this.newSummary,
        severity: this.newSeverity,
        reporterContact: this.newReporterContact,
        rawStatement: this.newRawStatement,
      })
      .subscribe(() => {
        this.newSummary = '';
        this.newSeverity = null;
        this.newReporterContact = '';
        this.newRawStatement = '';
        this.incidents.reload();
      });
  }
}
