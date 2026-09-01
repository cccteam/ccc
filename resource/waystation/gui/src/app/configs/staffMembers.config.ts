import { Resources, StaffMembers, Waystations } from '@app/service/zz_gen_constants';
import { enumeratedConfig, field, listViewConfig, rootConfig, section } from '@cccteam/ccc-lib/types';

// StaffMembers is the subject-value source: approvalLimit feeds the
// `totalCost <= subject.approvalLimit` condition on requisition approval.
export const staffMembersConfig = rootConfig({
  nav: {
    navItem: {
      label: 'Staff Members',
    },
    group: 'Command',
  },
  parentConfig: listViewConfig({
    title: 'Staff Members',
    createTitle: 'Staff Member',
    primaryResource: Resources.StaffMembers,
    listColumns: [
      { id: StaffMembers.fieldName.displayName },
      { id: StaffMembers.fieldName.userId },
      { id: StaffMembers.fieldName.approvalLimit },
      {
        id: StaffMembers.fieldName.homeWaystationId,
        header: 'Home Waystation',
        additionalIds: [
          {
            resource: Resources.Waystations,
            id: Waystations.fieldName.id,
            field: Waystations.fieldName.name,
          },
        ],
        concatFn: 'space-concat',
      },
    ],
    elements: [
      section({
        label: 'Identity',
        children: [
          field({
            name: StaffMembers.fieldName.displayName,
            label: 'Display Name',
            cols: 4,
          }),
          field({
            name: StaffMembers.fieldName.userId,
            label: 'User ID',
            cols: 4,
          }),
        ],
      }),
      section({
        label: 'Assignment',
        children: [
          field({
            name: StaffMembers.fieldName.homeWaystationId,
            label: 'Home Waystation',
            enumeratedConfig: enumeratedConfig({
              listDisplay: [Waystations.fieldName.name],
              viewDisplay: [Waystations.fieldName.name],
            }),
            cols: 4,
          }),
          field({
            name: StaffMembers.fieldName.approvalLimit,
            label: 'Approval Limit',
            cols: 4,
          }),
        ],
      }),
    ],
  }),
});
