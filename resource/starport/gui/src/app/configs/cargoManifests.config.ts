import { CargoManifests, Resources, Ships } from '@app/service/zz_gen_constants';
import { enumeratedConfig, field, listViewConfig, rootConfig, section } from '@cccteam/ccc-lib/types';

export const cargoManifestsConfig = rootConfig({
  nav: {
    navItem: {
      label: 'Cargo Manifests',
    },
    group: 'Logistics',
  },
  parentConfig: listViewConfig({
    title: 'Cargo Manifests',
    createTitle: 'Cargo Manifest',
    primaryResource: Resources.CargoManifests,
    listColumns: [
      {
        id: CargoManifests.fieldName.shipId,
        header: 'Ship',
        additionalIds: [
          {
            resource: Resources.Ships,
            id: Ships.fieldName.id,
            field: Ships.fieldName.name,
          },
        ],
        concatFn: 'space-concat',
      },
      { id: CargoManifests.fieldName.lineNumber },
      { id: CargoManifests.fieldName.details },
      { id: CargoManifests.fieldName.quantity },
      { id: CargoManifests.fieldName.declaredValue },
    ],
    elements: [
      section({
        label: 'Manifest Line',
        children: [
          field({
            name: CargoManifests.fieldName.shipId,
            label: 'Ship',
            enumeratedConfig: enumeratedConfig({
              listDisplay: [Ships.fieldName.name],
              viewDisplay: [Ships.fieldName.name],
            }),
            cols: 4,
          }),
          field({
            name: CargoManifests.fieldName.lineNumber,
            label: 'Line Number',
            cols: 4,
          }),
          field({
            name: CargoManifests.fieldName.details,
            label: 'Details',
            cols: 4,
          }),
          field({
            name: CargoManifests.fieldName.quantity,
            label: 'Quantity',
            cols: 4,
          }),
          field({
            name: CargoManifests.fieldName.declaredValue,
            label: 'Declared Value',
            cols: 4,
          }),
        ],
      }),
    ],
  }),
});
