import { DatePipe, DecimalPipe } from '@angular/common';
import { Component, computed, inject, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatSelectModule } from '@angular/material/select';
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
 * The template picker lists the BriefingTemplates catalog, the computed resource the
 * method's TemplateID names with a field-scope @enumerate: a request field naming rows
 * served from Go, listed here through the generated client under the caller's grant.
 *
 * Demonstrates: rpc.client-form, rpc.typed-result, rpc.dry-run, rpc.armed-read, @enumerate.computed.
 */
@Component({
  selector: 'app-briefing',
  imports: [
    DatePipe,
    DecimalPipe,
    MatButtonModule,
    MatCardModule,
    MatFormFieldModule,
    MatSelectModule,
    MatSlideToggleModule,
    StarChartComponent,
  ],
  templateUrl: './briefing.component.html',
  styleUrl: './briefing.component.scss',
})
export class BriefingComponent {
  sectors = inject(SectorService);

  sector = this.sectors.current;
  canCompile = computed(() => this.sectors.can(Permissions.Execute, Methods.CompileBriefing));
  includeHazards = signal(true);
  // The catalog the picker lists, through the generated handle for the resource the
  // metadata names (methodMeta's enumeratedResource for templateId is BriefingTemplates).
  templates = this.sectors.globalAll((api) => api.briefingTemplates);
  templateId = signal('standard');
  sheet = signal<CompileBriefingResult | undefined>(undefined);
  refusal = signal<string | undefined>(undefined);
  dryRunRefusal = signal<string | undefined>(undefined);

  async compile(): Promise<void> {
    this.refusal.set(undefined);
    try {
      this.sheet.set(
        await this.sectors
          .sectorApi()
          .compileBriefing.execute({ includeHazards: this.includeHazards(), templateId: this.templateId() }),
      );
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
      await this.sectors.sectorApi().compileBriefing.dryRun({ includeHazards: false, templateId: this.templateId() });
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
