import { PilotAssignments, Pilots, Resources, SquadronMemberships, Squadrons } from '@app/service/zz_gen_constants';
import {
  enumeratedConfig,
  field,
  foreignKeyDefault,
  listViewConfig,
  rootConfig,
  section,
} from '@cccteam/resource-angular/types';

// Pilots is the personnel registry and the subject-value anchor: each pilot's
// clearance and fee limit feed the `subject.clearance` and `subject.feeLimit`
// conditions on the flight deck. A pilot's page carries their assignments in the
// selected sector: the PilotAssignments view, the second view over SquadronMemberships.
// The view declares its backing table, a create goes into the table, and the new row
// shows up in the view on the next list because the view's SQL reads that table; the
// row route names the view's squadronId, whose declared enumeration is Squadrons, so a
// row opens the squadron's page, gated on Read of Squadrons in the sector.
//
// Demonstrates: @rowsOf.association, list.row-route.
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
  relatedConfigs: [
    listViewConfig({
      title: 'Assignments in the selected sector',
      createTitle: 'Assignment',
      primaryResource: Resources.PilotAssignments,
      parentRelation: { parentKey: Pilots.fieldName.userId, childKey: PilotAssignments.fieldName.userId },
      rowRoute: PilotAssignments.fieldName.squadronId,
      listColumns: [
        { id: PilotAssignments.fieldName.squadronName, header: 'Squadron' },
        { id: PilotAssignments.fieldName.wingName, header: 'Wing' },
      ],
      elements: [
        section({
          label: 'Membership',
          children: [
            field({
              name: SquadronMemberships.fieldName.squadronId,
              label: 'Squadron',
              cols: 6,
              enumeratedConfig: enumeratedConfig({
                listDisplay: [Squadrons.fieldName.name],
                viewDisplay: [Squadrons.fieldName.name],
              }),
            }),
            field({
              name: SquadronMemberships.fieldName.userId,
              label: 'Login',
              cols: 6,
              readOnly: true,
              default: foreignKeyDefault({ parentId: Pilots.fieldName.userId }),
            }),
          ],
        }),
      ],
    }),
  ],
});
