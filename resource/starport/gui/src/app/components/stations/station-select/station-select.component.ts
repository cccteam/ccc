import { Component, inject, OnInit, output, signal } from '@angular/core';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatSelectModule } from '@angular/material/select';
import { StationsService } from '../stations.service';

@Component({
  selector: 'app-station-select',
  imports: [MatFormFieldModule, MatSelectModule],
  templateUrl: './station-select.component.html',
})
export class StationSelectComponent implements OnInit {
  private stationsService = inject(StationsService);

  stations = signal<string[]>([]);
  stationChange = output<string>();

  ngOnInit(): void {
    this.stationsService.stations().subscribe((res) => {
      this.stations.set(res.stations ?? []);
      const first = this.stations()[0];
      if (first) {
        this.stationChange.emit(first);
      }
    });
  }
}
