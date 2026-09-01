import { Component, inject } from '@angular/core';
import { DatePipe } from '@angular/common';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatIconModule } from '@angular/material/icon';
import { MatTableModule } from '@angular/material/table';
import { CatalogItems, InventoryLots, Shipments } from '@app/service/zz_gen_resources';
import { WaystationService } from '../waystation.service';
import { WaystationSelectComponent } from '../waystation-select/waystation-select.component';

/**
 * Logistics pairs the quartermaster's two condition shapes: receiving a shipment is
 * gated on `arrivedAt IS NULL` (a second receive is refused), and deleting an
 * inventory lot is gated on its expiry date — lots that are fresh or have no expiry
 * at all refuse deletion for the quartermaster role.
 */
@Component({
  selector: 'app-logistics',
  imports: [DatePipe, MatButtonModule, MatCardModule, MatIconModule, MatTableModule, WaystationSelectComponent],
  templateUrl: './logistics.component.html',
  styleUrl: './logistics.component.scss',
})
export class LogisticsComponent {
  private ws = inject(WaystationService);

  shipments = this.ws.stationList<Shipments>('shipments');
  // Sorted server-side through the reserved sort parameter: soonest expiry first
  // (Spanner sorts NULL expiries — never expiring — to the top).
  lots = this.ws.stationList<InventoryLots>('inventory-lots?sort=expiresOn');
  catalogItems = this.ws.globalList<CatalogItems>('catalog-items');
  shipmentColumns = ['manifestCode', 'arrivedAt', 'shipmentActions'];
  lotColumns = ['item', 'quantity', 'expiresOn', 'binLocation', 'lotActions'];

  itemName(catalogItemId: string | undefined): string {
    return this.catalogItems.value().find((item) => item.id === catalogItemId)?.name ?? catalogItemId ?? '';
  }

  receive(shipment: Shipments): void {
    this.ws.receiveShipment(shipment.id).subscribe(() => this.shipments.reload());
  }

  removeLot(lot: InventoryLots): void {
    this.ws.deleteInventoryLot(lot.id).subscribe(() => this.lots.reload());
  }
}
