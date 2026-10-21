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
 * the server's answer. It is never switched to another sector.
 *
 * The one URL the guard rewrites names no sector at all: the library's config-driven pages
 * build their row links and their after-create navigation from the page's route as
 * written, `sectors/:sectorID/missions`, parameter and all, so a row opened from such a
 * page arrives here with the parameter's name where the sector should be. That link was
 * drawn on a deck in the open sector, so the guard sends it to the same page under that
 * sector; with no sector open it is refused like any other sector the session cannot
 * reach.
 *
 * Demonstrates: tenancy.concealed, tenancy.user-domains.
 */
export const sectorGuard: CanActivateFn = async (route, state) => {
  const sectors = inject(SectorService);
  const auth = inject(AuthService);
  const router = inject(Router);
  const sector = route.paramMap.get(DomainRouteParam) ?? '';
  const parameter = `:${DomainRouteParam}`;
  if (sector === parameter && sectors.current()) {
    return new RedirectCommand(router.parseUrl(state.url.replace(parameter, sectors.current())));
  }
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
 * guard writes the new sector; the one between is never cleared, so a link the deck drew
 * still knows the sector it was drawn in.
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
