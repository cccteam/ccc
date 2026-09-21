import { Resources, StandingOrders } from '@app/service/zz_gen_constants';
import { listViewConfig, rootConfig } from '@cccteam/resource-angular/types';

// StandingOrders is the book of standing orders: a @computed struct with no @primarykey,
// the KEY-LESS LIST. Its descriptor lists `keys: []` and the list operation alone, so the
// library's list page draws the whole book as one page: every line the server returned,
// identified by its position, the pager reading one page with Previous and Next disabled
// and the count, no View column, no create, no delete, and no row route (resourceRoutes
// builds none). Virtual scroll keeps a long book light; the size of a key-less list is
// the resource author's to keep manageable. A `pageSize` or `enableRowExpansion` on this
// config would fail at startup naming StandingOrders and the reason, since the page has
// no page size to set and no key to open a row by (standingOrders.config.spec.ts pins
// both refusals and the one request the page makes). The yeoman's first screen.
//
// Demonstrates: list.keyless.
export const standingOrdersConfig = rootConfig({
  nav: { navItem: { label: 'Standing Orders' }, group: 'Headquarters' },
  parentConfig: listViewConfig({
    title: 'Standing Orders',
    primaryResource: Resources.StandingOrders,
    enableVirtualScroll: true,
    listColumns: [{ id: StandingOrders.fieldName.section }, { id: StandingOrders.fieldName.directive }],
    elements: [],
  }),
});
