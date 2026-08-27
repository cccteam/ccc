import { AuthorizeLaunch, DockingBays, Methods, Resources, Ships } from '@app/service/zz_gen_constants';
import { enumeratedConfig, field, listViewConfig, rootConfig, rpcConfig, section } from '@cccteam/ccc-lib/types';

export const shipsConfig = rootConfig({
  nav: {
    navItem: {
      label: 'Ships',
    },
    group: 'Operations',
  },
  rpcConfigs: [
    rpcConfig({
      label: 'Authorize Launch',
      method: Methods.AuthorizeLaunch,
      conditions: [],
      elements: [
        field({
          name: AuthorizeLaunch.fieldName.launchCode,
          label: 'Launch Code',
        }),
      ],
      methodBodyTemplate: {
        shipId: { field: Ships.fieldName.id },
      },
      successMessage: 'Launch authorized',
    }),
  ],
  parentConfig: listViewConfig({
    title: 'Ships',
    createTitle: 'Ship',
    primaryResource: Resources.Ships,
    listColumns: [
      { id: Ships.fieldName.registryCode },
      { id: Ships.fieldName.name },
      {
        id: Ships.fieldName.dockingBayId,
        header: 'Docking Bay',
        additionalIds: [
          {
            resource: Resources.DockingBays,
            id: DockingBays.fieldName.id,
            field: DockingBays.fieldName.name,
          },
        ],
        concatFn: 'space-concat',
      },
      { id: Ships.fieldName.cargoValue },
    ],
    elements: [
      section({
        label: 'Registration',
        children: [
          field({
            name: Ships.fieldName.registryCode,
            label: 'Registry Code',
            cols: 4,
          }),
          field({
            name: Ships.fieldName.name,
            label: 'Name',
            cols: 4,
          }),
        ],
      }),
      section({
        label: 'Operations',
        children: [
          field({
            name: Ships.fieldName.dockingBayId,
            label: 'Docking Bay',
            enumeratedConfig: enumeratedConfig({
              listDisplay: [DockingBays.fieldName.name],
              viewDisplay: [DockingBays.fieldName.name],
            }),
            cols: 4,
          }),
          field({
            name: Ships.fieldName.cargoValue,
            label: 'Cargo Value',
            cols: 4,
          }),
          field({
            name: Ships.fieldName.updatedAt,
            label: 'Updated At',
            readOnly: true,
            cols: 4,
          }),
        ],
      }),
    ],
  }),
});
