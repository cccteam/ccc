import { ManifestLines, Resources } from '@app/service/zz_gen_constants';
import { listViewConfig, rootConfig } from '@cccteam/ccc-lib/types';

// ManifestLines is a virtual (subquery-backed) resource with a compound primary key:
// list-only, so the grid renders without a view button and there is no create/edit
// surface.
export const manifestLinesConfig = rootConfig({
  nav: {
    navItem: {
      label: 'Manifest Lines',
    },
    group: 'Reports',
  },
  parentConfig: listViewConfig({
    title: 'Manifest Lines',
    primaryResource: Resources.ManifestLines,
    showViewButton: false,
    listColumns: [
      { id: ManifestLines.fieldName.shipName, header: 'Ship' },
      { id: ManifestLines.fieldName.lineNumber, header: 'Line' },
      { id: ManifestLines.fieldName.details, header: 'Details' },
      { id: ManifestLines.fieldName.quantity, header: 'Quantity' },
      { id: ManifestLines.fieldName.declaredValue, header: 'Declared Value' },
    ],
    elements: [],
  }),
});
