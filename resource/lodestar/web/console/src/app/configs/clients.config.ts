import { Clients, Resources } from '@app/service/zz_gen_constants';
import { field, listViewConfig, rootConfig, section } from '@cccteam/ccc-lib/types';

// Clients demonstrates conditional row filtering on a global resource: the
// ClientBrowser role lists trusted clients only (`trusted = true`), while
// headquarters sees the full roster — same page, different rows per persona. Its
// contact fields are PII.
export const clientsConfig = rootConfig({
  nav: { navItem: { label: 'Clients' }, group: 'Headquarters' },
  parentConfig: listViewConfig({
    title: 'Clients',
    createTitle: 'Client',
    primaryResource: Resources.Clients,
    listColumns: [{ id: Clients.fieldName.name }, { id: Clients.piiFieldName.contactName }, { id: Clients.fieldName.trusted }],
    elements: [
      section({
        label: 'Client',
        children: [
          field({ name: Clients.fieldName.name, label: 'Name', cols: 4 }),
          field({ name: Clients.fieldName.trusted, label: 'Trusted', cols: 4 }),
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
