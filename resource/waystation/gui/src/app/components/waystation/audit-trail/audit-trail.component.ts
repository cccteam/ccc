import { Component, computed, inject } from '@angular/core';
import { DatePipe } from '@angular/common';
import { MatCardModule } from '@angular/material/card';
import { MatTableModule } from '@angular/material/table';
import { Permissions, Resources } from '@app/service/zz_gen_constants';
import { AuditTrailEntry, WaystationService } from '../waystation.service';

/**
 * The audit trail is the manual-resource demo: DataChangeEvents is library
 * infrastructure with no generated handler, so the API surface is a hand-written
 * route whose List permission was registered through @manualAddResource. The events
 * span the whole ring (they are not domain-scoped), so this page carries no
 * waystation selector. Only auditor-voss (RecordsAuditor) and the commander hold
 * the grant — for everyone else the digest says so up front, and the page explains
 * instead of asking the server for a refusal. The handle comes from the client's
 * escape hatch (WaystationService.auditTrail), described once, typed like the rest.
 */
@Component({
  selector: 'app-audit-trail',
  imports: [DatePipe, MatCardModule, MatTableModule],
  templateUrl: './audit-trail.component.html',
  styleUrl: './audit-trail.component.scss',
})
export class AuditTrailComponent {
  private ws = inject(WaystationService);

  entries = this.ws.globalList(() => this.ws.auditTrail);
  forbidden = computed(() => !this.ws.can(Permissions.List, Resources.AuditTrailEntries));
  columns = ['eventTime', 'tableName', 'rowId', 'eventSource', 'changeSet'];

  changeSetLabel(entry: AuditTrailEntry): string {
    return entry.changeSet ? JSON.stringify(entry.changeSet) : '—';
  }
}
