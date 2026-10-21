import { inject } from '@angular/core';
import { CanActivateFn, CanDeactivateFn, PRIMARY_OUTLET, RedirectCommand, Router } from '@angular/router';
import { apiDescriptor } from '@app/service/zz_gen_api';
import { DomainRouteParam } from '@app/service/zz_gen_resources';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { firstValueFrom } from 'rxjs';
import { refusedSegment, TenantService } from './tenant.service';

/**
 * tenantGuard sits on the tenant segment (`tenants/:tenantID`, the generated
 * DomainRoutePath) and is the one writer of the tenant the pages read. It takes the tenant
 * from the URL, checks it against the session's tenant list (the generated user-domains
 * answer: the same foothold test the server's domain guard applies, so the two never
 * disagree) and enters it before any page under the segment loads. A tenant not on the
 * list is asked of the server once more, since on a deep link the list may still be
 * loading, and a seat granted since sign-in is not on it yet; a tenant still absent is
 * refused: the browser is sent to the refusal page under the tenant's own URL, which shows
 * the server's answer. It is never switched to another tenant.
 */
export const tenantGuard: CanActivateFn = async (route) => {
  const tenants = inject(TenantService);
  const auth = inject(AuthService);
  const router = inject(Router);
  const tenant = route.paramMap.get(DomainRouteParam) ?? '';
  if (!tenants.tenants().includes(tenant)) {
    await firstValueFrom(auth.loadDomains());
  }
  if (!tenants.tenants().includes(tenant)) {
    return new RedirectCommand(router.createUrlTree(['/', apiDescriptor.domainRoute.segment, tenant, refusedSegment]));
  }
  tenants.enter(tenant);
  return true;
};

/**
 * tenantLeave clears the tenant when the URL leaves the tenant segment: a global page
 * has no tenant. Moving from one tenant's page to another's stays under the segment, and
 * the guard writes the new tenant; the one between is never cleared, so a link the page
 * drew still knows the tenant it was drawn in.
 */
export const tenantLeave: CanDeactivateFn<unknown> = (_component, _route, _state, next) => {
  const router = inject(Router);
  const tenants = inject(TenantService);
  const { segment } = apiDescriptor.domainRoute;
  const first = router.parseUrl(next?.url ?? '').root.children[PRIMARY_OUTLET]?.segments[0]?.path;
  if (first !== segment) {
    tenants.leave();
  }
  return true;
};
