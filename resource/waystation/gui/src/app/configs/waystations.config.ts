import { Methods, Resources, RunSafetyDrill, Waystations } from '@app/service/zz_gen_constants';
import { field, listViewConfig, rootConfig, rpcConfig, section } from '@cccteam/ccc-lib/types';

// Waystations is the tenant table itself: rows here ARE the permission domains,
// read at bootstrap by MigrateRoles and served live via /api/waystation-directory.
// The Run Safety Drill RPC is global Execute, gated for the SafetyOfficer role by
// a row-free time condition (`now < ...`).
export const waystationsConfig = rootConfig({
  nav: {
    navItem: {
      label: 'Waystations',
    },
    group: 'Command',
  },
  rpcConfigs: [
    rpcConfig({
      label: 'Run Safety Drill',
      method: Methods.RunSafetyDrill,
      conditions: [],
      elements: [
        field({
          name: RunSafetyDrill.fieldName.announcement,
          label: 'Announcement',
        }),
      ],
      methodBodyTemplate: {},
      successMessage: 'Safety drill announced',
    }),
  ],
  parentConfig: listViewConfig({
    title: 'Waystations',
    createTitle: 'Waystation',
    primaryResource: Resources.Waystations,
    listColumns: [
      { id: Waystations.fieldName.id },
      { id: Waystations.fieldName.name },
      { id: Waystations.fieldName.orbitBand },
      { id: Waystations.fieldName.commissioned },
    ],
    elements: [
      section({
        label: 'Station',
        children: [
          field({
            name: Waystations.fieldName.name,
            label: 'Name',
            cols: 4,
          }),
          field({
            name: Waystations.fieldName.orbitBand,
            label: 'Orbit Band',
            cols: 4,
          }),
          field({
            name: Waystations.fieldName.commissioned,
            label: 'Commissioned',
            cols: 4,
          }),
        ],
      }),
    ],
  }),
});
