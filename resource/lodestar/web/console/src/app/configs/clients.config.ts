import { Clients, Resources } from '@app/service/zz_gen_constants';
import { field, listViewConfig, nullBooleanConfig, rootConfig, section } from '@cccteam/resource-angular/types';

// Clients demonstrates conditional row filtering on a global resource: the
// ClientBrowser role lists trusted clients only (`trusted = true`), while
// headquarters sees the full roster — same page, different rows per persona. Its
// contact fields are PII.
//
// Insured is the demo's nullable BOOL: whether the outfit carries salvage cover, unknown
// until the underwriter answers, then yes or no. Its metadata says nullboolean, so the
// form renders the three-way picker, with the null state labelled in the story's word;
// the required Trusted checkbox beside it is the two-state contrast.
//
// Trusted is also the list's icon column: a verified badge for a trusted outfit, a caution
// for one not yet trusted, each with a sentence naming the client as its tooltip and its
// accessible name. The map is declared here and the data drives it; the ClientBrowser
// persona, who lists trusted clients alone, sees nothing but badges.
//
// The box searches the name, the two contact fields and the contact's display name
// through the child table, and sits beside the column filters; the request is placed as a
// whole. Headquarters, whose box reaches the PII fields, posts the filter with a name
// filter or a contact-name match beside the box, and a ClientBrowser persona, for whom the
// box searches the name alone, keeps the same shape in the URL and reads under the box
// which fields the role leaves out.
//
// Demonstrates: nullboolean, column.icon, search.beside-filters, search.beside-filters.pii.
export const clientsConfig = rootConfig({
  nav: { navItem: { label: 'Clients' }, group: 'Headquarters' },
  parentConfig: listViewConfig({
    title: 'Clients',
    createTitle: 'Client',
    primaryResource: Resources.Clients,
    listColumns: [
      { id: Clients.fieldName.name },
      { id: Clients.piiFieldName.contactName },
      {
        id: Clients.fieldName.trusted,
        header: 'Trust',
        iconConfig: {
          icons: { true: 'verified', false: 'gpp_maybe' },
          tooltips: { true: '{name} is a trusted client', false: '{name} is not yet trusted' },
        },
      },
    ],
    elements: [
      section({
        label: 'Client',
        children: [
          field({ name: Clients.fieldName.name, label: 'Name', cols: 4 }),
          field({ name: Clients.fieldName.trusted, label: 'Trusted', cols: 4 }),
          field({
            name: Clients.fieldName.insured,
            label: 'Insured',
            cols: 4,
            nullBooleanConfig: nullBooleanConfig({
              displayValues: {
                null: { label: 'Unknown', value: null },
                true: { label: 'Yes', value: true },
                false: { label: 'No', value: false },
              },
            }),
          }),
        ],
      }),
      section({
        label: 'Contact (PII)',
        children: [
          field({ name: Clients.piiFieldName.contactName, label: 'Contact name', cols: 4 }),
          field({ name: Clients.piiFieldName.contactEmail, label: 'Contact email', cols: 4 }),
        ],
      }),
    ],
  }),
});
