import { Resources, ShipCargoSummaries } from '@app/service/zz_gen_constants';
import { listViewConfig, rootConfig } from '@cccteam/ccc-lib/types';

// ShipCargoSummaries is a virtual (subquery-backed) resource: list-only, so the grid
// renders without a view button and there is no create/edit surface.
export const shipCargoSummariesConfig = rootConfig({
  nav: {
    navItem: {
      label: 'Ship Cargo Summaries',
    },
    group: 'Reports',
  },
  parentConfig: listViewConfig({
    title: 'Ship Cargo Summaries',
    primaryResource: Resources.ShipCargoSummaries,
    showViewButton: false,
    listColumns: [
      { id: ShipCargoSummaries.fieldName.shipName, header: 'Ship' },
      { id: ShipCargoSummaries.fieldName.dockingBayName, header: 'Docking Bay' },
      { id: ShipCargoSummaries.fieldName.manifestLines, header: 'Manifest Lines' },
      { id: ShipCargoSummaries.fieldName.totalDeclaredValue, header: 'Total Declared Value' },
    ],
    elements: [],
  }),
});
