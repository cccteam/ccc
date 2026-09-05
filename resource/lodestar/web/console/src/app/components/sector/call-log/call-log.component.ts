import { Component, computed, inject } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatTableModule } from '@angular/material/table';
import { DistressCalls as DistressCallFields, Permissions, Resources } from '@app/service/zz_gen_constants';
import { DistressCalls } from '@app/service/zz_gen_resources';
import { SectorService } from '../sector.service';
import { StarChartComponent } from '../star-chart/star-chart.component';

/**
 * The call log: incoming distress calls before they become missions, and the
 * create-form narrowing proof (F9). The form renders inputs from the digest's
 * field-level Create entries: the Cadet's partial-width grant covers summary and
 * severity, so their form is two inputs — the caller contact and transcript neither
 * render nor travel. The case number is server-issued; the transcript is write-only.
 */
@Component({
  selector: 'app-call-log',
  imports: [FormsModule, MatButtonModule, MatCardModule, MatFormFieldModule, MatInputModule, MatTableModule, StarChartComponent],
  templateUrl: './call-log.component.html',
  styleUrl: './call-log.component.scss',
})
export class CallLogComponent {
  private sectors = inject(SectorService);

  calls = this.sectors.sectorList((sector) => sector.distressCalls);
  columns = ['caseNumber', 'summary', 'severity', 'callerContact', 'filedBy'];

  sector = this.sectors.current;
  canList = computed(() => this.sectors.can(Permissions.List, Resources.DistressCalls));
  canFile = computed(() => this.sectors.can(Permissions.Create, Resources.DistressCalls));
  readonly fields = { ...DistressCallFields.fieldName, ...DistressCallFields.piiFieldName };

  creatableFields = computed(() => this.sectors.grantedFields(Permissions.Create, Resources.DistressCalls));

  canWrite(field: string): boolean {
    const creatable = this.creatableFields();
    return creatable === undefined || creatable.includes(field);
  }

  newSummary = '';
  newSeverity: number | null = null;
  newCallerContact = '';
  newTranscript = '';

  contactLabel(call: DistressCalls): string {
    return 'callerContact' in call ? (call.callerContact ?? '') : '— withheld —';
  }

  async file(): Promise<void> {
    if (!this.newSummary || this.newSeverity === null) return;
    await this.sectors.sectorApi().distressCalls.create({
      summary: this.newSummary,
      severity: this.newSeverity,
      ...(this.canWrite(this.fields.callerContact) ? { callerContact: this.newCallerContact } : {}),
      ...(this.canWrite(this.fields.transcript) ? { transcript: this.newTranscript } : {}),
    });
    this.newSummary = '';
    this.newSeverity = null;
    this.newCallerContact = '';
    this.newTranscript = '';
    this.calls.reload();
  }
}
