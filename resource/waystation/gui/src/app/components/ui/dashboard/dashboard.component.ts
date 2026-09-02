import { Component, inject } from '@angular/core';
import { MatCardModule } from '@angular/material/card';
import { MatTableModule } from '@angular/material/table';
import { RouterModule } from '@angular/router';
import { AuthService } from '@cccteam/ccc-lib/auth-service';
import { WaystationService } from '../../waystation/waystation.service';

@Component({
  selector: 'app-dashboard',
  imports: [MatCardModule, MatTableModule, RouterModule],
  templateUrl: './dashboard.component.html',
  styleUrl: './dashboard.component.scss',
})
export class DashboardComponent {
  auth = inject(AuthService);
  private ws = inject(WaystationService);

  // FleetSummaries is a computed resource aggregated across every waystation the
  // caller can see — the commander's fleet-wide view. The list gate is the handle's
  // own: only personas whose digest carries the List grant ask for it; everyone else
  // never issues the request.
  fleet = this.ws.globalList((api) => api.fleetSummaries);
  fleetColumns = ['name', 'openWorkOrders', 'pendingRequisitions'];
}
