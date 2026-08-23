import { DockingBays, Resources } from '@app/service/zz_gen_constants';
import { field, listViewConfig, rootConfig, section } from '@cccteam/ccc-lib/types';

export const dockingBaysConfig = rootConfig({
  nav: {
    navItem: {
      label: 'Docking Bays',
    },
    group: 'Operations',
  },
  parentConfig: listViewConfig({
    title: 'Docking Bays',
    createTitle: 'Docking Bay',
    primaryResource: Resources.DockingBays,
    listColumns: [
      { id: DockingBays.fieldName.name },
      { id: DockingBays.fieldName.deckLevel },
      { id: DockingBays.fieldName.maxTonnage },
    ],
    elements: [
      section({
        label: 'Bay Information',
        children: [
          field({
            name: DockingBays.fieldName.name,
            label: 'Name',
            cols: 4,
          }),
          field({
            name: DockingBays.fieldName.deckLevel,
            label: 'Deck Level',
            cols: 4,
          }),
          field({
            name: DockingBays.fieldName.maxTonnage,
            label: 'Max Tonnage',
            cols: 4,
          }),
        ],
      }),
    ],
  }),
});
