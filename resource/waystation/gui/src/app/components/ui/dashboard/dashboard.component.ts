import { httpResource } from '@angular/common/http';
import { Component, inject } from '@angular/core';
import { MatCardModule } from '@angular/material/card';
import { MatTableModule } from '@angular/material/table';
import { RouterModule } from '@angular/router';
import { Permissions, Resources } from '@app/service/zz_gen_constants';
import { FleetSummaries } from '@app/service/zz_gen_resources';
import { AuthService } from '@cccteam/ccc-lib/auth-service';
import { API_URL } from '@cccteam/ccc-lib/types';
import { WaystationService } from '@components/waystation/waystation.service';

@Component({
  selector: 'app-dashboard',
  imports: [MatCardModule, MatTableModule, RouterModule],
  templateUrl: './dashboard.component.html',
  styleUrl: './dashboard.component.scss',
})
export class DashboardComponent {
  auth = inject(AuthService);
  private ws = inject(WaystationService);
  private apiUrl = inject(API_URL);

  // FleetSummaries is a computed resource aggregated across every waystation the
  // caller can see — the commander's fleet-wide view. Only personas whose digest
  // carries the List grant ask for it; everyone else never issues the request.
  fleet = httpResource<FleetSummaries[]>(
    () => (this.ws.can(Permissions.List, Resources.FleetSummaries) ? `${this.apiUrl}/fleet-summaries` : undefined),
    { defaultValue: [] },
  );
  fleetColumns = ['name', 'openWorkOrders', 'pendingRequisitions'];
}
