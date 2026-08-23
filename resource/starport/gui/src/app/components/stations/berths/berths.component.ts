import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { MatSlideToggleModule } from '@angular/material/slide-toggle';
import { MatTableModule } from '@angular/material/table';
import { Berths } from '@app/service/zz_gen_resources';
import { StationsService } from '../stations.service';
import { StationSelectComponent } from '../station-select/station-select.component';

@Component({
  selector: 'app-berths',
  imports: [
    FormsModule,
    MatButtonModule,
    MatCardModule,
    MatFormFieldModule,
    MatIconModule,
    MatInputModule,
    MatSelectModule,
    MatSlideToggleModule,
    MatTableModule,
    StationSelectComponent,
  ],
  templateUrl: './berths.component.html',
  styleUrl: './berths.component.scss',
})
export class BerthsComponent {
  private stationsService = inject(StationsService);

  station = signal('');
  berths = signal<Berths[]>([]);
  columns = ['designation', 'sizeClass', 'occupied', 'actions'];

  newDesignation = '';
  newSizeClass: number | null = null;

  stationChanged(station: string): void {
    this.station.set(station);
    this.load();
  }

  load(): void {
    if (!this.station()) {
      return;
    }
    this.stationsService.berths(this.station()).subscribe({
      next: (berths) => this.berths.set(berths ?? []),
      error: () => this.berths.set([]),
    });
  }

  create(): void {
    if (!this.newDesignation || this.newSizeClass === null) {
      return;
    }
    this.stationsService
      .createBerth(this.station(), {
        designation: this.newDesignation,
        sizeClass: this.newSizeClass,
        occupied: false,
      })
      .subscribe(() => {
        this.newDesignation = '';
        this.newSizeClass = null;
        this.load();
      });
  }

  toggleOccupied(berth: Berths): void {
    this.stationsService.patchBerth(this.station(), berth.id, { occupied: !berth.occupied }).subscribe(() => {
      this.load();
    });
  }

  remove(berth: Berths): void {
    this.stationsService.removeBerth(this.station(), berth.id).subscribe(() => {
      this.load();
    });
  }
}
