import { Pilots, Resources } from '@app/service/zz_gen_constants';
import { field, listViewConfig, rootConfig, section } from '@cccteam/ccc-lib/types';

// Pilots is the personnel registry and the subject-value anchor: each pilot's
// clearance and fee limit feed the `subject.clearance` and `subject.feeLimit`
// conditions on the flight deck.
export const pilotsConfig = rootConfig({
  nav: { navItem: { label: 'Pilots' }, group: 'Headquarters' },
  parentConfig: listViewConfig({
    title: 'Pilots',
    createTitle: 'Pilot',
    primaryResource: Resources.Pilots,
    listColumns: [
      { id: Pilots.fieldName.displayName },
      { id: Pilots.fieldName.userId },
      { id: Pilots.fieldName.clearance },
      { id: Pilots.fieldName.feeLimit },
    ],
    elements: [
      section({
        label: 'Pilot',
        children: [
          field({ name: Pilots.fieldName.displayName, label: 'Name', cols: 3 }),
          field({ name: Pilots.fieldName.userId, label: 'Login', cols: 3 }),
          field({ name: Pilots.fieldName.clearance, label: 'Hazard clearance', cols: 3 }),
          field({ name: Pilots.fieldName.feeLimit, label: 'Fee limit', cols: 3 }),
        ],
      }),
    ],
  }),
});
