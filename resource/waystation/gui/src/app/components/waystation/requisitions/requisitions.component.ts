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
import { DeclineReason, RequisitionStatus } from '@app/service/zz_gen_enums';
import { CatalogItems, RequisitionLines, Requisitions } from '@app/service/zz_gen_resources';
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

  requisitions = this.ws.stationList<Requisitions>('requisitions');
  lineRows = this.ws.stationList<RequisitionLines>('requisition-lines');
  catalogItems = this.ws.globalList<CatalogItems>('catalog-items');
  columns = ['justification', 'status', 'requestedBy', 'totalCost', 'neededBy', 'actions'];
  lineColumns = ['item', 'quantity', 'unitCostSnapshot', 'lineActions'];

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

  create(): void {
    if (!this.newJustification || !this.newNeededBy) {
      return;
    }
    this.ws
      .createRequisition({
        // NeededBy is a DATE column; the date input's YYYY-MM-DD value is its wire format.
        justification: this.newJustification,
        neededBy: this.newNeededBy as unknown as Date,
      })
      .subscribe(() => {
        this.newJustification = '';
        this.newNeededBy = '';
        this.requisitions.reload();
      });
  }

  addLine(requisition: Requisitions): void {
    const item = this.catalogItems.value().find((candidate) => candidate.id === this.newLineItemId);
    if (!item || this.newLineQuantity === null) {
      return;
    }
    const nextNumber = Math.max(0, ...this.lines(requisition).map((line) => line.lineNumber)) + 1;
    this.ws
      .createRequisitionLine(requisition.id, nextNumber, {
        // The unit cost is snapshotted at add time: the line keeps the price the
        // requester saw even if the catalog moves later.
        catalogItemId: this.newLineItemId,
        quantity: this.newLineQuantity,
        unitCostSnapshot: item.unitCost,
      })
      .subscribe(() => {
        this.newLineItemId = '';
        this.newLineQuantity = null;
        this.lineRows.reload();
      });
  }

  removeLine(line: RequisitionLines): void {
    this.ws.removeRequisitionLine(line.requisitionId, line.lineNumber).subscribe(() => this.lineRows.reload());
  }

  // Submit recomputes the frozen total and moves the status, and the lines' cost
  // visibility can change with it — reload both lists.
  submit(requisition: Requisitions): void {
    this.ws.submitRequisition(requisition.id).subscribe(() => {
      this.requisitions.reload();
      this.lineRows.reload();
    });
  }

  approve(requisition: Requisitions): void {
    this.ws.approveRequisition(requisition.id).subscribe(() => {
      this.requisitions.reload();
      this.lineRows.reload();
    });
  }

  decline(requisition: Requisitions): void {
    if (!this.declineReason) {
      return;
    }
    this.ws.declineRequisition(requisition.id, this.declineReason).subscribe(() => {
      this.declineReason = '';
      this.requisitions.reload();
    });
  }
}
