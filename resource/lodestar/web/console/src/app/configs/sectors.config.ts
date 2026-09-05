import { Methods, Resources, IssueBulletin, Sectors } from '@app/service/zz_gen_constants';
import { field, listViewConfig, rootConfig, rpcConfig, section } from '@cccteam/ccc-lib/types';

// Sectors is the tenant table itself: rows here ARE the permission domains, read at
// bootstrap by MigrateRoles; the star chart's "chart every sector" roster comes from
// this resource's own generated (permission-checked) List. Issue Bulletin is the
// global, row-free Execute gated for the Bulletin Officer by `now < ...`.
export const sectorsConfig = rootConfig({
  nav: { navItem: { label: 'Sectors' }, group: 'Headquarters' },
  rpcConfigs: [
    rpcConfig({
      label: 'Issue Bulletin',
      method: Methods.IssueBulletin,
      conditions: [],
      elements: [field({ name: IssueBulletin.fieldName.announcement, label: 'Announcement' })],
      methodBodyTemplate: {},
      successMessage: 'Bulletin issued to every sector',
    }),
  ],
  parentConfig: listViewConfig({
    title: 'Sectors',
    createTitle: 'Sector',
    primaryResource: Resources.Sectors,
    listColumns: [
      { id: Sectors.fieldName.id },
      { id: Sectors.fieldName.name },
      { id: Sectors.fieldName.region },
      { id: Sectors.fieldName.established },
    ],
    elements: [
      section({
        label: 'Sector',
        children: [
          field({ name: Sectors.fieldName.name, label: 'Name', cols: 4 }),
          field({ name: Sectors.fieldName.region, label: 'Region', cols: 4 }),
          field({ name: Sectors.fieldName.established, label: 'Established', cols: 4 }),
        ],
      }),
    ],
  }),
});
