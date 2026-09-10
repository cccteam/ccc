import { BriefingTemplates, ClientRosters, Missions, Resources } from '@app/service/zz_gen_constants';
import { enumeratedConfig, field, listViewConfig, rootConfig, section } from '@cccteam/resource-angular/types';

// Missions is the config-driven page in the selected sector: the library's list, form,
// and pickers over a domain-scoped resource (RESOURCE_DOMAIN is the sector the topbar
// selects), where the flight deck is the hand-written deck over the same rows. Its two
// declared pickers list exactly what the generated metadata names — ClientRosters for
// the client key, a sector-scoped view over Clients carrying the contact count, and
// BriefingTemplates for the plain template column, a computed catalog with no read
// route, so a picked template's name resolves from the option list — and the
// configuration only narrows and presents those rows. A persona without List on a
// picker's resource sees that request refused, never a substitute resource.
//
// Demonstrates: picker.config-driven, @enumerate.plain-column, @enumerate.key-view, picker.read-disabled.
export const missionsConfig = rootConfig({
  nav: { navItem: { label: 'Missions (config page)' }, group: 'Sector Ops' },
  routeData: { route: 'sector/missions' },
  parentConfig: listViewConfig({
    title: 'Missions',
    createTitle: 'Mission',
    primaryResource: Resources.Missions,
    listColumns: [
      { id: Missions.fieldName.title },
      { id: Missions.fieldName.clientId },
      { id: Missions.fieldName.kindId },
      { id: Missions.fieldName.statusId },
      { id: Missions.fieldName.deadline },
      { id: Missions.fieldName.briefingTemplateId },
    ],
    elements: [
      section({
        label: 'Call sheet',
        children: [
          field({ name: Missions.fieldName.title, label: 'Title', cols: 6 }),
          field({
            name: Missions.fieldName.clientId,
            label: 'Client',
            cols: 6,
            enumeratedConfig: enumeratedConfig({
              listDisplay: [ClientRosters.fieldName.name, ClientRosters.fieldName.contactCount],
              viewDisplay: [ClientRosters.fieldName.name, ClientRosters.fieldName.contactCount],
              listConcatFn: 'hyphen-concat',
              viewConcatFn: 'hyphen-concat',
              sorts: [{ field: ClientRosters.fieldName.name, direction: 'asc' }],
            }),
          }),
          field({ name: Missions.fieldName.kindId, label: 'Kind', cols: 4 }),
          field({ name: Missions.fieldName.hazard, label: 'Hazard', cols: 4 }),
          field({ name: Missions.fieldName.fee, label: 'Fee', cols: 4 }),
          field({ name: Missions.fieldName.deadline, label: 'Deadline', cols: 6 }),
          field({
            name: Missions.fieldName.briefingTemplateId,
            label: 'Briefing template',
            cols: 6,
            enumeratedConfig: enumeratedConfig({
              listDisplay: [BriefingTemplates.fieldName.name],
              viewDisplay: [BriefingTemplates.fieldName.name],
              searchable: true,
            }),
          }),
          field({ name: Missions.fieldName.statusId, label: 'Status', cols: 4 }),
          field({ name: Missions.fieldName.brief, label: 'Brief', cols: 12 }),
          field({ name: Missions.fieldName.notes, label: 'Notes', cols: 12 }),
        ],
      }),
    ],
  }),
});
