import { Hangars, Resources, ShipClasses, Ships } from '@app/service/zz_gen_constants';
import {
  enumeratedConfig,
  field,
  listViewConfig,
  multiColumnConfig,
  rootConfig,
  section,
} from '@cccteam/resource-angular/types';

// Ships is the fleet page in the selected sector: the library's two picker read modes
// side by side on one form, and the two ways a list column shows a referenced resource,
// every one decided by the maximum page size the generated descriptor carries and never
// by a literal here. The hangar picker lists Hangars, which declares a maximum (@page
// max 200): the picker pages the hangars one server page at a time, Previous and Next
// inside the panel following the server's cursors, in the hangars' own @order, and reads
// the chosen hangar by key, so a ship berthed in a hangar on another page still shows
// its name. The class picker lists ShipClasses, the hull catalog, which declares no
// maximum: the picker reads the catalog whole with limit=all and resolves the chosen
// hull from that list, so the catalog needs no read route for it. The list's Hangar
// column resolves each page's hangar names with one filter=id:in:(...) request over the
// page's hangar keys, served by the key's index, where its Class column reads the
// catalog whole once and maps it for every page. Nothing on this page reads a bounded
// resource whole.
//
// Demonstrates: picker.paged, picker.whole, column.referenced-in.
export const shipsConfig = rootConfig({
  nav: { navItem: { label: 'Ships (config page)' }, group: 'Sector Ops' },
  routeData: { route: 'sector/ships' },
  parentConfig: listViewConfig({
    title: 'Ships',
    createTitle: 'Ship',
    primaryResource: Resources.Ships,
    listColumns: [
      { id: Ships.fieldName.name },
      { id: Ships.fieldName.registry },
      multiColumnConfig({
        id: Ships.fieldName.hangarId,
        header: 'Hangar',
        additionalIds: [{ id: Hangars.fieldName.id, resource: Resources.Hangars, field: Hangars.fieldName.name }],
        concatFn: 'hyphen-concat',
      }),
      multiColumnConfig({
        id: Ships.fieldName.classId,
        header: 'Class',
        additionalIds: [{ id: ShipClasses.fieldName.id, resource: Resources.ShipClasses, field: ShipClasses.fieldName.designation }],
        concatFn: 'hyphen-concat',
      }),
      { id: Ships.fieldName.lastRefitAt, header: 'Last refit' },
    ],
    elements: [
      section({
        label: 'Hull',
        children: [
          field({ name: Ships.fieldName.name, label: 'Name', cols: 4 }),
          field({ name: Ships.fieldName.registry, label: 'Registry', cols: 4 }),
          field({
            name: Ships.fieldName.classId,
            label: 'Class',
            cols: 4,
            enumeratedConfig: enumeratedConfig({
              listDisplay: [ShipClasses.fieldName.designation],
              viewDisplay: [ShipClasses.fieldName.designation],
            }),
          }),
        ],
      }),
      section({
        label: 'Berth',
        children: [
          field({
            name: Ships.fieldName.hangarId,
            label: 'Hangar',
            cols: 6,
            enumeratedConfig: enumeratedConfig({
              listDisplay: [Hangars.fieldName.name, Hangars.fieldName.zone],
              viewDisplay: [Hangars.fieldName.name, Hangars.fieldName.zone],
              listConcatFn: 'hyphen-concat',
              viewConcatFn: 'hyphen-concat',
            }),
          }),
        ],
      }),
    ],
  }),
});
