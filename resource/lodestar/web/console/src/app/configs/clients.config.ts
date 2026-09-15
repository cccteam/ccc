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
// Demonstrates: nullboolean.
export const clientsConfig = rootConfig({
  nav: { navItem: { label: 'Clients' }, group: 'Headquarters' },
  parentConfig: listViewConfig({
    title: 'Clients',
    createTitle: 'Client',
    primaryResource: Resources.Clients,
    listColumns: [
      { id: Clients.fieldName.name },
      { id: Clients.piiFieldName.contactName },
      { id: Clients.fieldName.trusted },
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
