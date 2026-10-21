import { inject } from '@angular/core';
import { CanActivateFn, CanDeactivateFn, PRIMARY_OUTLET, RedirectCommand, Router } from '@angular/router';
import { apiDescriptor } from '@app/service/zz_gen_api';
import { DomainRouteParam } from '@app/service/zz_gen_resources';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { Domain } from '@cccteam/resource';
import { firstValueFrom } from 'rxjs';
import { refusedSegment, SectorService } from './sector.service';

/**
 * sectorGuard sits on the sector segment (`sectors/:sectorID`, the generated
 * DomainRoutePath) and is the one writer of the sector the decks read. It takes the sector
 * from the URL, checks it against the session's sector list (the generated user-domains
 * answer: the same foothold test the server's domain guard applies, so the two never
 * disagree) and enters it before any deck under the segment loads. A sector not on the
 * list is asked of the server once more, since on a deep link the list may still be
 * loading, and a seat granted since sign-in is not on it yet; a sector still absent is
 * refused: the browser is sent to the refusal page under the sector's own URL, which shows
 * the server's answer. It is never switched to another sector, and no URL is rewritten:
 * the library's config-driven pages build their row links and after-create navigation
 * under the page's sector, so every link that arrives here names the sector it is for.
 *
 * Demonstrates: tenancy.concealed, tenancy.user-domains.
 */
export const sectorGuard: CanActivateFn = async (route) => {
  const sectors = inject(SectorService);
  const auth = inject(AuthService);
  const router = inject(Router);
  const sector = route.paramMap.get(DomainRouteParam) ?? '';
  if (!auth.domains().includes(sector as Domain)) {
    await firstValueFrom(auth.loadDomains());
  }
  if (!auth.domains().includes(sector as Domain)) {
    return new RedirectCommand(router.createUrlTree(['/', apiDescriptor.domainRoute.segment, sector, refusedSegment]));
  }
  sectors.enter(sector);
  return true;
};

/**
 * sectorLeave clears the sector when the URL leaves the sector segment: a global page has
 * no sector. Moving from one sector's deck to another's stays under the segment, and the
 * guard writes the new sector; nothing is cleared in between.
 */
export const sectorLeave: CanDeactivateFn<unknown> = (_component, _route, _state, next) => {
  const router = inject(Router);
  const sectors = inject(SectorService);
  const { segment } = apiDescriptor.domainRoute;
  const first = router.parseUrl(next?.url ?? '').root.children[PRIMARY_OUTLET]?.segments[0]?.path;
  if (first !== segment) {
    sectors.leave();
  }
  return true;
};
