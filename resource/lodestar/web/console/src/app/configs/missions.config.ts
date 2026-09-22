import { BriefingTemplates, ClientRosters, MissionBoards, Missions, Resources } from '@app/service/zz_gen_constants';
import { enumeratedConfig, field, listViewConfig, rootConfig, section } from '@cccteam/resource-angular/types';

// Missions is the config-driven page in the selected sector: the library's list, form,
// and pickers over a domain-scoped resource (RESOURCE_DOMAIN is the sector the topbar
// selects), where the flight deck is the hand-written deck over the same rows. The list
// is the MissionBoards view, which carries what Missions lacks: the client's name, the
// squadron's name, and the days left, computed in the SQL. The view declares its
// backing table, a create goes into the table, and the new row shows up in the view on
// the next list because the view's SQL reads that table: the page names the view alone,
// and the metadata's rowsOf sends its create, edit, and delete to Missions, whose form
// the elements build, and opens a row on the mission's own page by the key the view
// carries under the table's column name. Its two declared pickers list exactly what the
// generated metadata names — ClientRosters for the client key, a sector-scoped view
// over Clients carrying the contact count, and BriefingTemplates for the plain template
// column, a computed catalog with no read route, so a picked template's name resolves
// from the option list — and the configuration only narrows and presents those rows. A
// persona without List on the view, or on a picker's resource, sees that request
// refused, never a substitute resource.
//
// The list is paged by the server, and only there: the page asks for one page of the
// board at the size the view's descriptor declares (no pageSize here, so the @page
// default applies), the grid shows it with First, Previous, and Next following the
// server's cursors and the first page's total kept while turning, and a header click or
// a column filter is a request parameter, so the server orders and narrows every row of
// the board. The filter controls follow the generated metadata: the indexed columns
// (title, client, squadron, status, deadline) filter always, and days left, computed in
// the SQL and indexed by nothing but declared allow_filter, filters only beside one of
// them — its control waits, with a hint, until an indexed filter is in the request, and
// clearing the last indexed filter clears it too. A page size over the view's maximum,
// or a filter the server refuses, is shown in the server's words.
//
// The two pickers read on their sources' maximums, from the generated descriptor. The
// client roster declares one, so the client picker pages the roster one server page at
// a time, Previous and Next inside the panel, in the roster's own @order, and reads the
// chosen client by the roster's key. The briefing template catalog declares none, so the
// template picker reads it whole with limit=all and resolves the chosen sheet from that
// list, which is why the catalog needs no read route.
//
// Demonstrates: picker.config-driven, picker.paged, picker.whole, @enumerate.plain-column, @enumerate.key-view, picker.read-disabled, @rowsOf.same-row, list.server-paged, metadata.filterable.
export const missionsConfig = rootConfig({
  nav: { navItem: { label: 'Missions (config page)' }, group: 'Sector Ops' },
  routeData: { route: 'sector/missions' },
  parentConfig: listViewConfig({
    title: 'Missions',
    createTitle: 'Mission',
    primaryResource: Resources.MissionBoards,
    listColumns: [
      { id: MissionBoards.fieldName.title },
      { id: MissionBoards.fieldName.clientName, header: 'Client' },
      { id: MissionBoards.fieldName.squadronName, header: 'Squadron' },
      { id: MissionBoards.fieldName.statusId },
      { id: MissionBoards.fieldName.deadline },
      { id: MissionBoards.fieldName.daysLeft, header: 'Days left' },
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
