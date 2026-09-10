import { DatePipe, DecimalPipe } from '@angular/common';
import { Component, computed, inject, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatSlideToggleModule } from '@angular/material/slide-toggle';
import { Methods, Permissions } from '@app/service/zz_gen_constants';
import { CompileBriefingResult } from '@app/service/zz_gen_methods';
import { ApiError } from '@cccteam/resource';
import { SectorService } from '../sector.service';
import { StarChartComponent } from '../star-chart/star-chart.component';

/**
 * The sector briefing: the one method that runs outside a transaction. CompileBriefing
 * takes a resource.Client instead of a transaction, reads the sector's missions AS THE
 * CALLER (so the Marshal's sheet carries every fee and the Archivist's counts
 * redactions), asks as data whether the hazard board may be folded in, and answers a
 * typed sheet. Its Dry-run switch is greyed: there is no transaction to roll back, and
 * the server refuses the header with 400, which the switch shows rather than hides.
 *
 * Demonstrates: rpc.client-form, rpc.typed-result, rpc.dry-run, rpc.armed-read.
 */
@Component({
  selector: 'app-briefing',
  imports: [DatePipe, DecimalPipe, MatButtonModule, MatCardModule, MatSlideToggleModule, StarChartComponent],
  templateUrl: './briefing.component.html',
  styleUrl: './briefing.component.scss',
})
export class BriefingComponent {
  sectors = inject(SectorService);

  sector = this.sectors.current;
  canCompile = computed(() => this.sectors.can(Permissions.Execute, Methods.CompileBriefing));
  includeHazards = signal(true);
  sheet = signal<CompileBriefingResult | undefined>(undefined);
  refusal = signal<string | undefined>(undefined);
  dryRunRefusal = signal<string | undefined>(undefined);

  async compile(): Promise<void> {
    this.refusal.set(undefined);
    try {
      this.sheet.set(await this.sectors.sectorApi().compileBriefing.execute({ includeHazards: this.includeHazards() }));
    } catch (e) {
      if (e instanceof ApiError) {
        this.refusal.set(`${e.status}: ${e.message}`);
        return;
      }
      throw e;
    }
  }

  /** The refused dry run, shown as the proof: a client-form method has nothing to roll back. */
  async tryDryRun(): Promise<void> {
    this.dryRunRefusal.set(undefined);
    try {
      await this.sectors.sectorApi().compileBriefing.dryRun({ includeHazards: false });
      this.dryRunRefusal.set('the server accepted a dry run of a client-form method; it should not');
    } catch (e) {
      if (e instanceof ApiError) {
        this.dryRunRefusal.set(`${e.status}: ${e.message}`);
        return;
      }
      throw e;
    }
  }
}
