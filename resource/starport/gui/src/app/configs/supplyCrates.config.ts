import { Resources, Ships, SupplyCrates } from '@app/service/zz_gen_constants';
import { enumeratedConfig, field, listViewConfig, rootConfig, section } from '@cccteam/ccc-lib/types';

export const supplyCratesConfig = rootConfig({
  nav: {
    navItem: {
      label: 'Supply Crates',
    },
    group: 'Logistics',
  },
  parentConfig: listViewConfig({
    title: 'Supply Crates',
    createTitle: 'Supply Crate',
    primaryResource: Resources.SupplyCrates,
    listColumns: [
      { id: SupplyCrates.fieldName.label },
      { id: SupplyCrates.fieldName.quantity },
      { id: SupplyCrates.fieldName.priority },
      { id: SupplyCrates.fieldName.status },
      { id: SupplyCrates.fieldName.barcode },
    ],
    elements: [
      section({
        label: 'Crate',
        children: [
          field({
            name: SupplyCrates.fieldName.label,
            label: 'Label',
            cols: 4,
          }),
          field({
            name: SupplyCrates.fieldName.quantity,
            label: 'Quantity',
            cols: 4,
          }),
          field({
            name: SupplyCrates.fieldName.priority,
            label: 'Priority',
            cols: 4,
          }),
          field({
            name: SupplyCrates.fieldName.status,
            label: 'Status',
            cols: 4,
          }),
        ],
      }),
      section({
        label: 'Tracking',
        children: [
          field({
            // Barcode is output only: the server assigns it on create and it can never
            // be written, which the generated permission metadata already encodes.
            name: SupplyCrates.fieldName.barcode,
            label: 'Barcode',
            readOnly: true,
            cols: 4,
          }),
          field({
            // Notes is input only: accepted on mutations, never returned by reads.
            name: SupplyCrates.fieldName.notes,
            label: 'Notes (input only)',
            cols: 4,
          }),
          field({
            name: SupplyCrates.piiFieldName.inspectorBadge,
            label: 'Inspector Badge (PII)',
            cols: 4,
          }),
          field({
            name: SupplyCrates.fieldName.assignedShipId,
            label: 'Assigned Ship',
            enumeratedConfig: enumeratedConfig({
              listDisplay: [Ships.fieldName.name],
              viewDisplay: [Ships.fieldName.name],
            }),
            cols: 4,
          }),
        ],
      }),
    ],
  }),
});
