import { computed, effect, inject, Injectable, signal, untracked } from '@angular/core';
import { PRIMARY_OUTLET, Router, UrlSegment, UrlSegmentGroup } from '@angular/router';
import { injectApi } from '@app/api/api';
import { Api, apiDescriptor } from '@app/service/zz_gen_api';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { storeSignal } from '@cccteam/resource-angular/resource-client';
import { Domain, Method, Permission, PermissionDigestState, Resource } from '@cccteam/resource';

/** The segment under a tenant where a URL naming a tenant the session cannot reach lands. */
export const refusedSegment = 'refused';

/** The page a tenant opens on when the picker is used from the refusal page. */
const landing = 'dashboard';

/**
 * TenantService holds the one piece of state the tenant-scoped pages share — the tenant
 * the page is open in — and binds the generated API client to it. The tenant is the
 * permission domain for every request those pages make.
 *
 * The tenant is a consequence of the route, never a choice. Every tenant-scoped page lives
 * under the tenant segment (`tenants/:tenantID`, the generated DomainRoutePath, mirroring
 * the API's), the tenant guard on that segment writes `current` from the URL before the
 * page loads and clears it when the URL leaves the segment, and nothing else writes it. The
 * header's picker sets no tenant: it navigates to the same page under the chosen tenant
 * (`open`) and the guard does the rest. So a link to a tenant's page can be shared, and a
 * bookmark lands where it points.
 *
 * Permissions: the client owns the digest cache — @cccteam/resource-angular's AuthService, guard, and
 * directive answer from the same cache. Entering a tenant loads that tenant's digest;
 * `can` and `state` answer from it reactively.
 */
@Injectable({ providedIn: 'root' })
export class TenantService {
  /** The generated client: global handles on the root, tenant handles under domain(). */
  readonly api: Api = injectApi();

  private auth = inject(AuthService);
  private router = inject(Router);

  // The digest cache mirrored into a signal, so computeds re-evaluate when a digest loads.
  private permissions = storeSignal(this.api.permissions.snapshot);

  /** The tenants the session holds at least one grant in, from the user-domains endpoint. */
  readonly tenants = computed<readonly string[]>(() => this.auth.domains());

  // The tenant the URL names: the tenant guard's alone to write (enter, leave).
  private routed = signal('');

  /** The tenant the page is open in; empty outside the tenant segment. */
  readonly current = this.routed.asReadonly();

  constructor() {
    // Entering a tenant re-scopes every permission question, so load that tenant's
    // digest into the client's cache. The load runs untracked so the effect never
    // adopts the interceptor's loading-signal reads as dependencies.
    effect(() => {
      const tenant = this.current();
      if (!tenant) return;
      untracked(() => void this.api.permissions.loadDigest(tenant as Domain).catch(() => undefined));
    });
  }

  /**
   * enter is the tenant guard's write, and nobody else's: the guard calls it with the
   * tenant the URL names once the session is known to reach it, before any page under the
   * segment loads.
   */
  enter(tenant: string): void {
    this.routed.set(tenant);
  }

  /** leave is the guard's other write: the URL has left the tenant segment. */
  leave(): void {
    this.routed.set('');
  }

  /**
   * open navigates to the page the browser is on, under the given tenant: the picker's
   * action. A page under a tenant keeps its path with the tenant replaced; a page outside
   * one enters the tenant on that path (`/users` opens `/tenants/acme/users`); the refusal
   * page opens the tenant's landing. Query and fragment travel along, and the guard on the
   * segment settles the tenant from the new URL.
   */
  open(tenant: string): Promise<boolean> {
    const { segment } = apiDescriptor.domainRoute;
    const tree = this.router.parseUrl(this.router.url);
    const primary = tree.root.children[PRIMARY_OUTLET];
    const segments = primary?.segments ?? [];
    const underTenant = segments[0]?.path === segment && segments.length > 1;
    let page = underTenant ? segments.slice(2) : segments;
    if (page[0]?.path === refusedSegment) {
      page = [new UrlSegment(landing, {})];
    }
    const under = [new UrlSegment(segment, {}), new UrlSegment(tenant, {}), ...page];
    if (primary) {
      primary.segments = under;
    } else {
      tree.root.children[PRIMARY_OUTLET] = new UrlSegmentGroup(under, {});
    }
    return this.router.navigateByUrl(tree);
  }

  /**
   * can answers one permission question from the digest. The client resolves the scope:
   * a global resource asks the global digest, a tenant-scoped resource or method asks
   * the open tenant's. Conditional grants answer true — render, and let the server
   * narrow per row.
   */
  can(permission: Permission, target: Resource | Method): boolean {
    this.permissions();
    return this.api.can(permission, target, (this.current() || undefined) as Domain | undefined);
  }

  /** The digest's tri-state for one target in its scope. */
  state(permission: Permission, target: Resource | Method): PermissionDigestState | undefined {
    this.permissions();
    const tenant = this.current() || undefined;
    const scope = this.api.descriptor.resources[target]?.scope ?? this.api.descriptor.methods[target]?.scope;
    if (scope === 'domain' && !tenant) return undefined;
    return this.api.permissions.state({
      resource: target,
      permission,
      domain: scope === 'domain' ? (tenant as Domain) : undefined,
    });
  }
}
