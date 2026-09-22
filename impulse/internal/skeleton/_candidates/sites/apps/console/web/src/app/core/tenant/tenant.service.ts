import { computed, effect, inject, Injectable, linkedSignal, untracked } from '@angular/core';
import { injectApi } from '@app/api/api';
import { Api } from '@app/service/zz_gen_api';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { storeSignal } from '@cccteam/resource-angular/resource-client';
import { Domain, Method, Permission, PermissionDigestState, Resource } from '@cccteam/resource';

/**
 * TenantService holds the one piece of state the tenant-scoped pages share — the
 * selected tenant — and binds the generated API client to it. The tenant is the
 * permission domain for every request those pages make.
 *
 * Permissions: the client owns the digest cache — @cccteam/resource-angular's AuthService, guard, and
 * directive answer from the same cache. Selecting a tenant loads that tenant's digest;
 * `can` and `state` answer from it reactively.
 */
@Injectable({ providedIn: 'root' })
export class TenantService {
  /** The generated client: global handles on the root, tenant handles under domain(). */
  readonly api: Api = injectApi();

  private auth = inject(AuthService);

  // The digest cache mirrored into a signal, so computeds re-evaluate when a digest loads.
  private permissions = storeSignal(this.api.permissions.snapshot);

  /** The tenants the session holds at least one grant in, from the user-domains endpoint. */
  readonly tenants = computed<readonly string[]>(() => this.auth.domains());

  // current keeps the user's choice while the list still offers it and snaps to the
  // first offered tenant when the list changes underneath it.
  readonly current = linkedSignal<readonly string[], string>({
    source: this.tenants,
    computation: (tenants, previous) =>
      previous !== undefined && tenants.includes(previous.value) ? previous.value : (tenants[0] ?? ''),
  });

  constructor() {
    // Selecting a tenant re-scopes every permission question, so load that tenant's
    // digest into the client's cache. The load runs untracked so the effect never
    // adopts the interceptor's loading-signal reads as dependencies.
    effect(() => {
      const tenant = this.current();
      if (!tenant) return;
      untracked(() => void this.api.permissions.loadDigest(tenant as Domain).catch(() => undefined));
    });
  }

  select(tenant: string): void {
    this.current.set(tenant);
  }

  /**
   * can answers one permission question from the digest. The client resolves the scope:
   * a global resource asks the global digest, a tenant-scoped resource or method asks
   * the selected tenant's. Conditional grants answer true — render, and let the server
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
