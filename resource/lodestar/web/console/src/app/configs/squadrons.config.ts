import { Resources, SquadronMemberships, SquadronRosters, Squadrons, Wings } from '@app/service/zz_gen_constants';
import { Squadrons as SquadronRow } from '@app/service/zz_gen_resources';
import {
  arrayConfig,
  componentConfig,
  enumeratedConfig,
  field,
  foreignKeyDefault,
  listViewConfig,
  rootConfig,
  section,
} from '@cccteam/resource-angular/types';
import { SquadronChannelComponent } from '@components/sector/squadron-channel/squadron-channel.component';

// Squadrons is a config-driven page in the selected sector whose row page carries the
// squadron's roster: the SquadronRosters view over SquadronMemberships, one row per
// membership with the pilot's display name from the personnel registry. The view
// declares its backing table, a create goes into the table, and the new row shows up in
// the view on the next list because the view's SQL reads that table: the roster names
// the view alone, its create form is SquadronMemberships' (the squadron filled from the
// page, the login typed), and a row is deleted from the list through the table by the
// compound key the view carries under the table's own column names. The row route
// names the roster's pilotId, whose declared enumeration is Pilots: a row opens the
// pilot's page by that value, gated on Read of Pilots.
//
// The row page also carries the two related-config shapes that are not a list: the
// squadron's channel card, the application's own component handed the row
// (componentConfig), and the sector's other squadrons, the same resource filtered by
// the page's row and each drawn as its own form (arrayConfig).
//
// Demonstrates: @rowsOf.association, list.row-route, config.component, config.array.
export const squadronsConfig = rootConfig({
  nav: { navItem: { label: 'Squadrons (config page)' }, group: 'Sector Ops' },
  routeData: { route: 'sector/squadrons' },
  parentConfig: listViewConfig({
    title: 'Squadrons',
    createTitle: 'Squadron',
    primaryResource: Resources.Squadrons,
    listColumns: [{ id: Squadrons.fieldName.name }, { id: Squadrons.fieldName.wingId }],
    elements: [
      section({
        label: 'Squadron',
        children: [
          field({ name: Squadrons.fieldName.name, label: 'Name', cols: 6 }),
          field({
            name: Squadrons.fieldName.wingId,
            label: 'Wing',
            cols: 6,
            enumeratedConfig: enumeratedConfig({
              listDisplay: [Wings.fieldName.name],
              viewDisplay: [Wings.fieldName.name],
            }),
          }),
        ],
      }),
    ],
  }),
  relatedConfigs: [
    // The channel card: rendered in the row's page with the squadron as its parentData.
    componentConfig({ primaryResource: Resources.Squadrons, component: SquadronChannelComponent }),
    listViewConfig({
      title: 'Roster',
      createTitle: 'Pilot',
      primaryResource: Resources.SquadronRosters,
      parentRelation: { parentKey: Squadrons.fieldName.id, childKey: SquadronRosters.fieldName.squadronId },
      rowRoute: SquadronRosters.fieldName.pilotId,
      listColumns: [
        { id: SquadronRosters.fieldName.pilotName, header: 'Pilot' },
        { id: SquadronRosters.fieldName.userId, header: 'Login' },
      ],
      elements: [
        section({
          label: 'Membership',
          children: [
            field({
              name: SquadronMemberships.fieldName.squadronId,
              label: 'Squadron',
              cols: 6,
              readOnly: true,
              default: foreignKeyDefault({ parentId: Squadrons.fieldName.id }),
            }),
            field({ name: SquadronMemberships.fieldName.userId, label: 'Login', cols: 6 }),
          ],
        }),
      ],
    }),
    // The sector's other squadrons: the listFilter excludes the page's row by the primary
    // key, indexed and so filterable everywhere, and the iterated config draws each row
    // as its own form. How the rows are read is the descriptor's statement, not this
    // config's: Squadrons declares a maximum page size, so the library asks one server
    // page at the descriptor's default and turns it with Previous and Next; a resource
    // declaring none would be read whole.
    arrayConfig({
      title: 'Other squadrons in this sector',
      primaryResource: Resources.Squadrons,
      listFilter: (squadron: SquadronRow): string => `${Squadrons.fieldName.id}:ne:${squadron.id}`,
      iteratedConfig: listViewConfig({
        primaryResource: Resources.Squadrons,
        listColumns: [{ id: Squadrons.fieldName.name }, { id: Squadrons.fieldName.wingId }],
        elements: [
          section({
            label: 'Squadron',
            children: [
              field({ name: Squadrons.fieldName.name, label: 'Name', cols: 6 }),
              field({
                name: Squadrons.fieldName.wingId,
                label: 'Wing',
                cols: 6,
                enumeratedConfig: enumeratedConfig({
                  listDisplay: [Wings.fieldName.name],
                  viewDisplay: [Wings.fieldName.name],
                }),
              }),
            ],
          }),
        ],
      }),
    }),
  ],
});
