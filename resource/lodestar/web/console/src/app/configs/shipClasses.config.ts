import { Resources, ShipClasses } from '@app/service/zz_gen_constants';
import { field, listViewConfig, rootConfig, section } from '@cccteam/ccc-lib/types';

// ShipClasses is the hull catalog: the global table Ship's `shipRole` attribute reaches
// through a join path. Designation is immutable.
export const shipClassesConfig = rootConfig({
  nav: { navItem: { label: 'Ship Classes' }, group: 'Headquarters' },
  parentConfig: listViewConfig({
    title: 'Ship Classes',
    createTitle: 'Ship Class',
    primaryResource: Resources.ShipClasses,
    listColumns: [
      { id: ShipClasses.fieldName.designation },
      { id: ShipClasses.fieldName.roleId },
      { id: ShipClasses.fieldName.tonnage },
      { id: ShipClasses.fieldName.hardened },
    ],
    elements: [
      section({
        label: 'Hull',
        children: [
          field({ name: ShipClasses.fieldName.designation, label: 'Designation', cols: 3 }),
          field({ name: ShipClasses.fieldName.roleId, label: 'Role', cols: 3 }),
          field({ name: ShipClasses.fieldName.tonnage, label: 'Tonnage', cols: 3 }),
          field({ name: ShipClasses.fieldName.hardened, label: 'Radiation hardened', cols: 3 }),
        ],
      }),
    ],
  }),
});
