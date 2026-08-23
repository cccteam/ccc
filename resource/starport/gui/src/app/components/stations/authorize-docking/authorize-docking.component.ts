import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { Berths } from '@app/service/zz_gen_resources';
import { AlertType } from '@cccteam/ccc-lib/types';
import { NotificationService } from '@cccteam/ccc-lib/ui-notification-service';
import { StationsService } from '../stations.service';
import { StationSelectComponent } from '../station-select/station-select.component';

@Component({
  selector: 'app-authorize-docking',
  imports: [
    FormsModule,
    MatButtonModule,
    MatCardModule,
    MatFormFieldModule,
    MatInputModule,
    MatSelectModule,
    StationSelectComponent,
  ],
  templateUrl: './authorize-docking.component.html',
  styleUrl: './authorize-docking.component.scss',
})
export class AuthorizeDockingComponent {
  private stationsService = inject(StationsService);
  private notifications = inject(NotificationService);

  station = signal('');
  berths = signal<Berths[]>([]);

  berthId = '';
  dockingCode = '';

  stationChanged(station: string): void {
    this.station.set(station);
    this.berthId = '';
    this.stationsService.berths(station).subscribe({
      next: (berths) => this.berths.set(berths ?? []),
      error: () => this.berths.set([]),
    });
  }

  authorize(): void {
    this.stationsService.authorizeDocking(this.station(), this.berthId, this.dockingCode).subscribe(() => {
      this.notifications.addGlobalNotification({
        message: `Docking authorized in ${this.station()}`,
        type: AlertType.SUCCESS,
        duration: 5000,
        link: '',
      });
      this.dockingCode = '';
    });
  }
}
