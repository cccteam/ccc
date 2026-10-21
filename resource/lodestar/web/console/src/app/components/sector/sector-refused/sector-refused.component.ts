import { Component, input, resource } from '@angular/core';
import { RouterLink } from '@angular/router';
import { injectApi } from '@app/api/api';
import { apiDescriptor } from '@app/service/zz_gen_api';
import { ApiError, Domain, ResourceDescriptor } from '@cccteam/resource';

/** The server's answer about a sector: whether it serves it to the session, in its words. */
export interface SectorAnswer {
  served: boolean;
  message: string;
}

/**
 * The refusal page: where a URL naming a sector the session cannot reach lands, under the
 * sector's own URL — a dark star opened from the chart, or a link to a sector you hold
 * nothing in. It asks the server what a deck there would have asked — one row of the first
 * sector-scoped resource the API lists — and shows the answer in the server's words: with
 * concealed domains, the 404 every route under the sector answers, the same for a sector
 * that does not exist and for one the session holds nothing in. The session is never
 * switched to another sector; the star chart is the way into one.
 *
 * Demonstrates: tenancy.concealed, star-chart.
 */
@Component({
  selector: 'app-sector-refused',
  imports: [RouterLink],
  templateUrl: './sector-refused.component.html',
  styleUrl: './sector-refused.component.scss',
})
export class SectorRefusedComponent {
  private api = injectApi();

  /** The sector the URL names, bound from the route. */
  sectorID = input.required<string>();

  /** The sector segment, for the link into the sector should the server serve it after all. */
  readonly segment = apiDescriptor.domainRoute.segment;

  // The question: one row of the first sector-scoped resource the API lists.
  private probe = Object.values(this.api.descriptor.resources).find(
    (descriptor): descriptor is ResourceDescriptor =>
      descriptor.scope === 'domain' && descriptor.operations.includes('list'),
  );

  /** The server's answer for the sector. */
  answer = resource({
    params: () => ({ sector: this.sectorID() }),
    loader: ({ params }): Promise<SectorAnswer> => this.ask(params.sector),
  });

  private async ask(sector: string): Promise<SectorAnswer> {
    const probe = this.probe;
    if (!probe) {
      return { served: false, message: 'the API lists nothing under a sector' };
    }
    try {
      await this.api.define<Record<string, unknown>, string[], 'list'>(probe, sector as Domain).list({ limit: 1 });
      return { served: true, message: '' };
    } catch (err) {
      if (err instanceof ApiError) {
        // 404 is the domain guard's answer; anything else means the sector is served.
        return { served: err.status !== 404, message: `${err.status}: ${err.message}` };
      }
      throw err;
    }
  }
}
