import { DistressCalls, Resources } from '@app/service/zz_gen_constants';
import { field, listViewConfig, rootConfig, section } from '@cccteam/resource-angular/types';

// Calls is the config-driven page over the distress calls in the selected sector, beside
// the hand-written call log over the same rows. It carries the write-only field and an
// imported object type. Transcript is input_only on the struct, writeOnly in the
// metadata: the server accepts it on create and update and never returns it, so the row
// page draws nothing for it in view mode and the create form draws a blank input whose
// value travels only when typed; a list column naming it would fail when the page is
// built, since a list never returns it, so none does. The row page's edit mode draws the
// same blank input: the row is read with its capability envelope, which the server plans
// over every field the caller may write, projected or not, so a caller whose Update grant
// covers the transcript sees it named although no read returns it, and the library takes
// the envelope as the positive list of editable fields. The hand-written call log writes
// the transcript too.
// Position is a GeoJSON Point, a type whose TypeScript shape is imported from the
// geojson package: the list's cell shows the JSON on one line, the row page prints it
// indented, and no mode offers an input; a call relayed by voice has none, the
// placeholder. The case number and the filer are server-owned and read-only everywhere.
//
// Demonstrates: field.object, field.write-only.
export const distressCallsConfig = rootConfig({
  nav: { navItem: { label: 'Calls (config page)' }, group: 'Sector Ops' },
  routeData: { route: 'sector/calls' },
  parentConfig: listViewConfig({
    title: 'Distress calls',
    createTitle: 'Distress call',
    primaryResource: Resources.DistressCalls,
    listColumns: [
      { id: DistressCalls.fieldName.caseNumber, header: 'Case' },
      { id: DistressCalls.fieldName.summary },
      { id: DistressCalls.fieldName.severity },
      { id: DistressCalls.fieldName.filedBy, header: 'Filed by' },
      { id: DistressCalls.fieldName.position },
    ],
    elements: [
      section({
        label: 'Call',
        children: [
          field({ name: DistressCalls.fieldName.summary, label: 'Summary', cols: 8 }),
          field({ name: DistressCalls.fieldName.severity, label: 'Severity', cols: 4 }),
          field({ name: DistressCalls.piiFieldName.callerContact, label: 'Caller contact', cols: 6 }),
          field({ name: DistressCalls.fieldName.transcript, label: 'Transcript', cols: 6 }),
          field({ name: DistressCalls.fieldName.caseNumber, label: 'Case number', cols: 6 }),
          field({ name: DistressCalls.fieldName.filedBy, label: 'Filed by', cols: 6 }),
          field({ name: DistressCalls.fieldName.position, label: 'Position', cols: 12 }),
        ],
      }),
    ],
  }),
});
