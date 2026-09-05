import { DecimalPipe } from '@angular/common';
import { Component, computed, inject } from '@angular/core';
import { MatSlideToggleModule } from '@angular/material/slide-toggle';
import { MatTooltipModule } from '@angular/material/tooltip';
import { SectorService } from '../sector.service';

/**
 * The star chart is the sector picker drawn as a small constellation: lit sectors are
 * the answer from the generated user-domains endpoint, the rest are dark. The "chart
 * every sector" toggle adds the full roster from the permission-checked Sectors
 * resource so you can click a dark one and watch every deck refuse fail-closed. The
 * shift clock beside it says which of the two hangar-deck shifts is on watch now on
 * the fleet's operations clock (America/Denver), and why.
 */
@Component({
  selector: 'app-star-chart',
  imports: [DecimalPipe, MatSlideToggleModule, MatTooltipModule],
  templateUrl: './star-chart.component.html',
  styleUrl: './star-chart.component.scss',
})
export class StarChartComponent {
  sectors = inject(SectorService);

  // Fixed constellation positions for the three seeded sectors; any others fall
  // along the bottom.
  private readonly positions: Record<string, { x: number; y: number }> = {
    anvil: { x: 60, y: 70 },
    bastion: { x: 180, y: 40 },
    cinder: { x: 290, y: 90 },
  };

  stars = computed(() =>
    this.sectors.sectors().map((sector, i) => ({
      sector,
      lit: this.sectors.lit().includes(sector),
      selected: this.sectors.current() === sector,
      ...(this.positions[sector] ?? { x: 40 + i * 90, y: 130 }),
    })),
  );

  // The shift clock: 06:00–18:00 headquarters time is Dockmaster Dara's watch,
  // 18:00–06:00 is Night Watch Nadia's — the wall-clock windows on their grants.
  shift = computed(() => {
    const hour = Number(
      new Intl.DateTimeFormat('en-US', { hour: 'numeric', hour12: false, timeZone: 'America/Denver' }).format(new Date()),
    );
    const day = hour >= 6 && hour < 18;
    return {
      onWatch: day ? 'Dockmaster Dara (day shift)' : 'Night Watch Nadia (night shift)',
      window: day ? '06:00–18:00' : '18:00–06:00',
      hour,
    };
  });
}
