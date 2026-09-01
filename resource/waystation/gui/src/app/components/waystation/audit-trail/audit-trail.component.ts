import { Component, inject, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { MatCardModule } from '@angular/material/card';
import { MatTableModule } from '@angular/material/table';
import { AuditTrailEntry, WaystationService } from '../waystation.service';

/**
 * The audit trail is the manual-resource demo: DataChangeEvents is library
 * infrastructure with no generated handler, so the API surface is a hand-written
 * route whose List permission was registered through @manualAddResource. The events
 * span the whole ring (they are not domain-scoped), so this page carries no
 * waystation selector. Only auditor-voss (RecordsAuditor) and the commander hold
 * the grant — everyone else sees the refusal below.
 */
@Component({
  selector: 'app-audit-trail',
  imports: [DatePipe, MatCardModule, MatTableModule],
  templateUrl: './audit-trail.component.html',
  styleUrl: './audit-trail.component.scss',
})
export class AuditTrailComponent {
  private ws = inject(WaystationService);

  entries = signal<AuditTrailEntry[]>([]);
  forbidden = signal(false);
  columns = ['eventTime', 'tableName', 'rowId', 'eventSource', 'changeSet'];

  constructor() {
    this.ws.auditTrail().subscribe({
      next: (entries) => {
        this.entries.set(entries ?? []);
        this.forbidden.set(false);
      },
      error: (err: { status?: number }) => {
        this.entries.set([]);
        this.forbidden.set(err.status === 403);
      },
    });
  }

  changeSetLabel(entry: AuditTrailEntry): string {
    return entry.changeSet ? JSON.stringify(entry.changeSet) : '—';
  }
}
