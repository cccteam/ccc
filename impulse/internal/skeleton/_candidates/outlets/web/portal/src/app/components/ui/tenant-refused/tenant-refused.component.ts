import { Component, input, resource } from '@angular/core';
import { RouterLink } from '@angular/router';
import { injectApi } from '@app/api/api';
import { apiDescriptor } from '@app/service/zz_gen_api';
import { ApiError, Domain, ResourceDescriptor } from '@cccteam/resource';

/** The server's answer about a tenant: whether it serves it to the session, in its words. */
export interface TenantAnswer {
  served: boolean;
  message: string;
}

/**
 * The refusal page: where a URL naming a tenant the session cannot reach lands, under the
 * tenant's own URL. It asks the server what a page there would have asked — one row of the
 * first tenant-scoped resource the API lists — and shows the answer in the server's words:
 * with concealed domains, the 404 every route under the tenant answers, the same for a
 * tenant that does not exist and for one the session holds nothing in. The session is
 * never switched to another tenant; the header's picker is the way into one.
 */
@Component({
  selector: 'app-tenant-refused',
  imports: [RouterLink],
  templateUrl: './tenant-refused.component.html',
  styleUrl: './tenant-refused.component.scss',
})
export class TenantRefusedComponent {
  private api = injectApi();

  /** The tenant the URL names, bound from the route. */
  tenantID = input.required<string>();

  /** The tenant segment, for the link into the tenant should the server serve it after all. */
  readonly segment = apiDescriptor.domainRoute.segment;

  // The question: one row of the first tenant-scoped resource the API lists.
  private probe = Object.values(this.api.descriptor.resources).find(
    (descriptor): descriptor is ResourceDescriptor =>
      descriptor.scope === 'domain' && descriptor.operations.includes('list'),
  );

  /** The server's answer for the tenant. */
  answer = resource({
    params: () => ({ tenant: this.tenantID() }),
    loader: ({ params }): Promise<TenantAnswer> => this.ask(params.tenant),
  });

  private async ask(tenant: string): Promise<TenantAnswer> {
    const probe = this.probe;
    if (!probe) {
      return { served: false, message: 'the API lists nothing under a tenant' };
    }
    try {
      await this.api.define<Record<string, unknown>, string[], 'list'>(probe, tenant as Domain).list({ limit: 1 });
      return { served: true, message: '' };
    } catch (err) {
      if (err instanceof ApiError) {
        // 404 is the domain guard's answer; anything else means the tenant is served.
        return { served: err.status !== 404, message: `${err.status}: ${err.message}` };
      }
      throw err;
    }
  }
}
