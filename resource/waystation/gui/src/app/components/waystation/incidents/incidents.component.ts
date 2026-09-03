import { Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatTableModule } from '@angular/material/table';
import { changes } from '@cccteam/resource';
import { IncidentReports as IncidentReportsFields, Permissions, Resources } from '@app/service/zz_gen_constants';
import { IncidentReports } from '@app/service/zz_gen_resources';
import { WaystationService } from '../waystation.service';
import { WaystationSelectComponent } from '../waystation-select/waystation-select.component';

/**
 * Incident reports demonstrate the server-owned field shapes in one form: the case
 * number is output-only (defaulted server-side, unwritable from the wire — and absent
 * from IncidentReportsCreate), the reporter contact is PII the auditor role reads
 * around (the column is simply absent from their rows), and severity is clamped by
 * an update-defaults hook. The report form also narrows per persona: the digest's
 * field-level Create entries decide which inputs render, so the technician (whose
 * grant covers summary and severity only) files reports through a two-input form.
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

  // The capability envelope rides each row so the edit affordance is the row's own
  // answer (the auditor reads incidents but edits nothing).
  incidents = this.ws.stationList((station) => station.incidentReports, { capabilities: ['Update'] });
  columns = ['caseNumber', 'summary', 'severity', 'reporterContact', 'actions'];

  // Affordances from the selected station's digest.
  station = this.ws.current;
  canList = computed(() => this.ws.can(Permissions.List, Resources.IncidentReports));
  canReport = computed(() => this.ws.can(Permissions.Create, Resources.IncidentReports));

  readonly fields = { ...IncidentReportsFields.fieldName, ...IncidentReportsFields.piiFieldName };

  // The digest's field-level Create entries narrow the report form: an input renders
  // only for a field the persona may supply — the technician's grant covers summary
  // and severity, so the PII contact and raw statement neither render nor travel.
  // An undefined answer means the digest carries no field information: nothing narrows.
  creatableFields = computed(() => this.ws.grantedFields(Permissions.Create, Resources.IncidentReports));

  canWrite(field: string): boolean {
    const creatable = this.creatableFields();
    return creatable === undefined || creatable.includes(field);
  }

  newSummary = '';
  newSeverity: number | null = null;
  newReporterContact = '';
  newRawStatement = '';

  // contactLabel renders the PII column: an absent key means this persona's grant
  // excludes the field — render it withheld, never blank-as-if-empty.
  contactLabel(incident: IncidentReports): string {
    return 'reporterContact' in incident ? (incident.reporterContact ?? '') : '— withheld —';
  }

  // Inline edit state for the selected incident.
  editingID = signal<string | undefined>(undefined);
  editing = computed(() => this.incidents.value().find((incident) => incident.id === this.editingID()));
  editSummary = '';
  editSeverity: number | null = null;

  canEdit(incident: IncidentReports): boolean {
    return this.ws.stationApi().incidentReports.rowCan(incident, 'Update');
  }

  edit(incident: IncidentReports): void {
    this.editingID.set(incident.id);
    this.editSummary = incident.summary ?? '';
    this.editSeverity = incident.severity ?? null;
  }

  cancelEdit(): void {
    this.editingID.set(undefined);
  }

  // The save diffs the edited image against the listed row through the client's
  // changes() helper: an untouched field never travels, an empty diff sends no
  // request, and a diff on a non-patchable field throws instead of silently saving
  // something other than what the form shows.
  async saveEdits(incident: IncidentReports): Promise<void> {
    const handle = this.ws.stationApi().incidentReports;
    const diff = changes(handle, incident, {
      summary: this.editSummary,
      severity: this.editSeverity ?? undefined,
    });
    if (diff) {
      await handle.patch(handle.keyOf(incident), diff);
    }
    this.editingID.set(undefined);
    this.incidents.reload();
  }

  async report(): Promise<void> {
    if (!this.newSummary || this.newSeverity === null) {
      return;
    }
    await this.ws.stationApi().incidentReports.create({
      summary: this.newSummary,
      severity: this.newSeverity,
      ...(this.canWrite(this.fields.reporterContact) ? { reporterContact: this.newReporterContact } : {}),
      ...(this.canWrite(this.fields.rawStatement) ? { rawStatement: this.newRawStatement } : {}),
    });
    this.newSummary = '';
    this.newSeverity = null;
    this.newReporterContact = '';
    this.newRawStatement = '';
    this.incidents.reload();
  }
}
