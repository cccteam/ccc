import { CrewMembers, Resources, Ships } from '@app/service/zz_gen_constants';
import { enumeratedConfig, field, listViewConfig, rootConfig, section } from '@cccteam/ccc-lib/types';

export const crewMembersConfig = rootConfig({
  nav: {
    navItem: {
      label: 'Crew Members',
    },
    group: 'Operations',
  },
  parentConfig: listViewConfig({
    title: 'Crew Members',
    createTitle: 'Crew Member',
    primaryResource: Resources.CrewMembers,
    listColumns: [
      { id: CrewMembers.fieldName.name },
      { id: CrewMembers.fieldName.rank },
      {
        id: CrewMembers.fieldName.shipId,
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
      { id: CrewMembers.fieldName.clearanceLevel },
    ],
    elements: [
      section({
        label: 'Crew Member',
        children: [
          field({
            name: CrewMembers.fieldName.name,
            label: 'Name',
            cols: 4,
          }),
          field({
            name: CrewMembers.fieldName.rank,
            label: 'Rank',
            cols: 4,
          }),
          field({
            name: CrewMembers.fieldName.shipId,
            label: 'Ship',
            enumeratedConfig: enumeratedConfig({
              listDisplay: [Ships.fieldName.name],
              viewDisplay: [Ships.fieldName.name],
            }),
            cols: 4,
          }),
        ],
      }),
      section({
        label: 'Restricted',
        children: [
          field({
            name: CrewMembers.fieldName.clearanceLevel,
            label: 'Clearance Level',
            cols: 4,
          }),
          field({
            name: CrewMembers.piiFieldName.medicalNotes,
            label: 'Medical Notes (PII)',
            cols: 8,
          }),
        ],
      }),
    ],
  }),
});
