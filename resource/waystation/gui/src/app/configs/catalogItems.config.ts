import { CatalogItems, Resources } from '@app/service/zz_gen_constants';
import { field, listViewConfig, rootConfig, section } from '@cccteam/ccc-lib/types';

export const catalogItemsConfig = rootConfig({
  nav: {
    navItem: {
      label: 'Catalog Items',
    },
    group: 'Supply Chain',
  },
  parentConfig: listViewConfig({
    title: 'Catalog Items',
    createTitle: 'Catalog Item',
    primaryResource: Resources.CatalogItems,
    listColumns: [
      { id: CatalogItems.fieldName.sku },
      { id: CatalogItems.fieldName.name },
      { id: CatalogItems.fieldName.categoryId, header: 'Category' },
      { id: CatalogItems.fieldName.unitCost },
      { id: CatalogItems.fieldName.hazardous },
    ],
    elements: [
      section({
        label: 'Item',
        children: [
          field({
            name: CatalogItems.fieldName.sku,
            label: 'SKU',
            cols: 4,
          }),
          field({
            name: CatalogItems.fieldName.name,
            label: 'Name',
            cols: 4,
          }),
          field({
            name: CatalogItems.fieldName.categoryId,
            label: 'Category',
            cols: 4,
          }),
        ],
      }),
      section({
        label: 'Procurement',
        children: [
          field({
            name: CatalogItems.fieldName.unitCost,
            label: 'Unit Cost',
            cols: 4,
          }),
          field({
            name: CatalogItems.fieldName.hazardous,
            label: 'Hazardous',
            cols: 4,
          }),
        ],
      }),
    ],
  }),
});
