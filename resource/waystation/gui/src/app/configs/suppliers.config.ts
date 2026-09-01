import { Resources, Suppliers } from '@app/service/zz_gen_constants';
import { field, listViewConfig, rootConfig, section } from '@cccteam/ccc-lib/types';

// Suppliers demonstrates conditional row filtering on a global resource: the
// VendorBrowser role lists active suppliers only, while VendorManager sees the
// full roster — same page, different rows per persona.
export const suppliersConfig = rootConfig({
  nav: {
    navItem: {
      label: 'Suppliers',
    },
    group: 'Supply Chain',
  },
  parentConfig: listViewConfig({
    title: 'Suppliers',
    createTitle: 'Supplier',
    primaryResource: Resources.Suppliers,
    listColumns: [
      { id: Suppliers.fieldName.name },
      { id: Suppliers.piiFieldName.contactName },
      { id: Suppliers.fieldName.active },
    ],
    elements: [
      section({
        label: 'Supplier',
        children: [
          field({
            name: Suppliers.fieldName.name,
            label: 'Name',
            cols: 4,
          }),
          field({
            name: Suppliers.fieldName.active,
            label: 'Active',
            cols: 4,
          }),
        ],
      }),
      section({
        label: 'Contact (PII)',
        children: [
          field({
            name: Suppliers.piiFieldName.contactName,
            label: 'Contact Name',
            cols: 4,
          }),
          field({
            name: Suppliers.piiFieldName.contactEmail,
            label: 'Contact Email',
            cols: 4,
          }),
        ],
      }),
    ],
  }),
});
