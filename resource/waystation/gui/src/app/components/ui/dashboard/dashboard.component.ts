import { HttpClient } from '@angular/common/http';
import { Component, inject, signal } from '@angular/core';
import { MatCardModule } from '@angular/material/card';
import { MatTableModule } from '@angular/material/table';
import { RouterModule } from '@angular/router';
import { FleetSummaries } from '@app/service/zz_gen_resources';
import { AuthService } from '@cccteam/ccc-lib/auth-service';
import { API_URL } from '@cccteam/ccc-lib/types';

@Component({
  selector: 'app-dashboard',
  imports: [MatCardModule, MatTableModule, RouterModule],
  templateUrl: './dashboard.component.html',
  styleUrl: './dashboard.component.scss',
})
export class DashboardComponent {
  auth = inject(AuthService);
  private http = inject(HttpClient);
  private apiUrl = inject(API_URL);

  // FleetSummaries is a computed resource aggregated across every waystation the
  // caller can see — the commander's fleet-wide view.
  fleet = signal<FleetSummaries[]>([]);
  fleetColumns = ['name', 'openWorkOrders', 'pendingRequisitions'];

  constructor() {
    this.http.get<FleetSummaries[]>(`${this.apiUrl}/fleet-summaries`).subscribe({
      next: (rows) => this.fleet.set(rows ?? []),
      error: () => this.fleet.set([]),
    });
  }
}
