import { Component, computed, inject, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { MatTableModule } from '@angular/material/table';
import { Method } from '@cccteam/resource';
import { Methods, Permissions, Resources } from '@app/service/zz_gen_constants';
import { DeclineReason, RequisitionStatus } from '@app/service/zz_gen_enums';
import { RequisitionLines, Requisitions } from '@app/service/zz_gen_resources';
import { WaystationService } from '../waystation.service';
import { WaystationSelectComponent } from '../waystation-select/waystation-select.component';

/**
 * The requisition flow walks the second stateful resource end to end: draft lines
 * are editable, Submit recomputes the server-owned total, approval is gated by the
 * approver's own approval limit (a subject value), and the line unit-cost column is
 * masked per cell — a persona without the cost grant receives rows WITHOUT the key,
 * rendered here as an em-dash, never a zero.
 */
@Component({
  selector: 'app-requisitions',
  imports: [
    DatePipe,
    FormsModule,
    MatButtonModule,
    MatCardModule,
    MatFormFieldModule,
    MatIconModule,
    MatInputModule,
    MatSelectModule,
    MatTableModule,
    WaystationSelectComponent,
  ],
  templateUrl: './requisitions.component.html',
  styleUrl: './requisitions.component.scss',
})
export class RequisitionsComponent {
  private ws = inject(WaystationService);

  readonly status = RequisitionStatus;
  readonly declineReasons = Object.values(DeclineReason);

  readonly methods = Methods;

  // The capability envelope rides every requisition row: Execute answers which
  // declared transitions apply to its state, and Create which member resources may
  // be created beneath it — the same answers the server enforces.
  requisitions = this.ws.stationList((station) => station.requisitions, { capabilities: ['Execute', 'Create'] });
  // Lines opt into the Delete envelope: the draft-only rule lives in the Foreman's
  // conditional grant, so each row answers whether it may be removed.
  lineRows = this.ws.stationList((station) => station.requisitionLines, { capabilities: ['Delete'] });
  catalogItems = this.ws.globalList((api) => api.catalogItems);
  columns = ['justification', 'status', 'requestedBy', 'totalCost', 'neededBy', 'actions'];
  lineColumns = ['item', 'quantity', 'unitCostSnapshot', 'lineActions'];

  // Affordances from the selected station's digest: an absent grant hides the
  // surface, a conditional one renders it and lets the server narrow per row.
  station = this.ws.current;
  canList = computed(() => this.ws.can(Permissions.List, Resources.Requisitions));
  canCreate = computed(() => this.ws.can(Permissions.Create, Resources.Requisitions));

  // Per-row affordances from the capability envelope: no statusId comparisons here —
  // whether a transition button renders is the row's own answer.
  canRun(requisition: Requisitions, method: Method): boolean {
    return this.ws.stationApi().requisitions.rowCan(requisition, 'Execute', method);
  }

  // The add-line form rides the row's Create affordance: the draft-only rule lives
  // in the Foreman's conditional RequisitionLines Create grant, evaluated against
  // this requisition's state — no hand-copied status check.
  canAddLine(requisition: Requisitions): boolean {
    return this.ws.stationApi().requisitions.rowCan(requisition, 'Create', Resources.RequisitionLines);
  }

  canRemove(line: RequisitionLines): boolean {
    return this.ws.stationApi().requisitionLines.rowCan(line, 'Delete');
  }

  linesByRequisition = computed(() => {
    const grouped = new Map<string, RequisitionLines[]>();
    for (const line of this.lineRows.value()) {
      const list = grouped.get(line.requisitionId) ?? [];
      list.push(line);
      grouped.set(line.requisitionId, list);
    }
    for (const list of grouped.values()) {
      list.sort((a, b) => a.lineNumber - b.lineNumber);
    }
    return grouped;
  });

  // The selection resolves against the live list: a station switch or reload that
  // drops the row clears it naturally.
  selectedID = signal<string | undefined>(undefined);
  selected = computed(() => this.requisitions.value().find((requisition) => requisition.id === this.selectedID()));

  declineReason = '';

  // New requisition form state.
  newJustification = '';
  newNeededBy = '';

  // New line form state for the selected draft.
  newLineItemId = '';
  newLineQuantity: number | null = null;

  select(requisition: Requisitions): void {
    this.selectedID.set(this.selectedID() === requisition.id ? undefined : requisition.id);
    this.declineReason = '';
    this.newLineItemId = '';
    this.newLineQuantity = null;
  }

  lines(requisition: Requisitions): RequisitionLines[] {
    return this.linesByRequisition().get(requisition.id) ?? [];
  }

  itemName(catalogItemId: string | undefined): string {
    return this.catalogItems.value().find((item) => item.id === catalogItemId)?.name ?? catalogItemId ?? '';
  }

  // costLabel renders the per-cell masked column: an absent key means the engine
  // withheld the value for this row — never render it as zero.
  costLabel(line: RequisitionLines): string {
    return 'unitCostSnapshot' in line ? String(line.unitCostSnapshot) : '—';
  }

  async create(): Promise<void> {
    if (!this.newJustification || !this.newNeededBy) {
      return;
    }
    await this.ws.stationApi().requisitions.create({
      justification: this.newJustification,
      // NeededBy is a DATE column; the date input's YYYY-MM-DD value is its wire format.
      neededBy: this.newNeededBy as unknown as Date,
    });
    this.newJustification = '';
    this.newNeededBy = '';
    this.requisitions.reload();
  }

  // Lines carry a compound, client-assigned key: RequisitionLinesCreate requires both
  // parts, and the client lifts them out of the value into the operation path.
  async addLine(requisition: Requisitions): Promise<void> {
    const item = this.catalogItems.value().find((candidate) => candidate.id === this.newLineItemId);
    if (!item || this.newLineQuantity === null || item.unitCost === undefined) {
      return;
    }
    const nextNumber = Math.max(0, ...this.lines(requisition).map((line) => line.lineNumber)) + 1;
    await this.ws.stationApi().requisitionLines.create({
      requisitionId: requisition.id,
      lineNumber: nextNumber,
      // The unit cost is snapshotted at add time: the line keeps the price the
      // requester saw even if the catalog moves later.
      catalogItemId: this.newLineItemId,
      quantity: this.newLineQuantity,
      unitCostSnapshot: item.unitCost,
    });
    this.newLineItemId = '';
    this.newLineQuantity = null;
    this.lineRows.reload();
  }

  async removeLine(line: RequisitionLines): Promise<void> {
    const lines = this.ws.stationApi().requisitionLines;
    await lines.remove(lines.keyOf(line));
    this.lineRows.reload();
  }

  // Submit recomputes the frozen total and moves the status, and the lines' cost
  // visibility can change with it — reload both lists.
  async submit(requisition: Requisitions): Promise<void> {
    await this.ws.stationApi().submitRequisition.execute({ requisitionId: requisition.id });
    this.requisitions.reload();
    this.lineRows.reload();
  }

  async approve(requisition: Requisitions): Promise<void> {
    await this.ws.stationApi().approveRequisition.execute({ requisitionId: requisition.id });
    this.requisitions.reload();
    this.lineRows.reload();
  }

  async decline(requisition: Requisitions): Promise<void> {
    if (!this.declineReason) {
      return;
    }
    await this.ws
      .stationApi()
      .declineRequisition.execute({ requisitionId: requisition.id, reason: this.declineReason });
    this.declineReason = '';
    this.requisitions.reload();
  }
}
