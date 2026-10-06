import { MissionDocuments, Missions, Resources } from '@app/service/zz_gen_constants';
import { enumeratedConfig, field, listViewConfig, rootConfig, section } from '@cccteam/resource-angular/types';

// Documents is the config-driven page over the mission documents in the selected sector,
// the register the marshal fills through the @upload method and the registrar keeps. It
// carries the two field shapes a form never types. Digest is the SHA-256 of the stored
// bytes, a BYTES(32) column: the list's cell shows its size (32 B), never the base64, and
// the row page shows the size with a download of the bytes named after the field; no
// mode offers an input, since binary content is written through @upload and served
// through @file. Provenance is a plain struct on a JSON column, its TypeScript interface
// derived by the generator: the cell shows the JSON on one line and the row page prints
// it indented; no mode offers an input, and a null provenance is the placeholder. The
// resource has no create, so the page has no create button, and a document's file is
// downloaded through the generated file route, not from this page.
//
// Demonstrates: field.bytes, field.object.
export const missionDocumentsConfig = rootConfig({
  nav: { navItem: { label: 'Documents (config page)' }, group: 'Sector Ops' },
  routeData: { route: 'sector/documents' },
  parentConfig: listViewConfig({
    title: 'Documents',
    primaryResource: Resources.MissionDocuments,
    listColumns: [
      { id: MissionDocuments.fieldName.title },
      { id: MissionDocuments.fieldName.fileName, header: 'File' },
      { id: MissionDocuments.fieldName.size },
      { id: MissionDocuments.fieldName.uploadedAt, header: 'Uploaded' },
      { id: MissionDocuments.fieldName.digest },
      { id: MissionDocuments.fieldName.provenance },
    ],
    elements: [
      section({
        label: 'Document',
        children: [
          field({
            name: MissionDocuments.fieldName.missionId,
            label: 'Mission',
            cols: 6,
            enumeratedConfig: enumeratedConfig({
              listDisplay: [Missions.fieldName.title],
              viewDisplay: [Missions.fieldName.title],
            }),
          }),
          field({ name: MissionDocuments.fieldName.title, label: 'Title', cols: 6 }),
          field({ name: MissionDocuments.fieldName.fileName, label: 'File name', cols: 4 }),
          field({ name: MissionDocuments.fieldName.contentType, label: 'Content type', cols: 4 }),
          field({ name: MissionDocuments.fieldName.size, label: 'Size', cols: 4 }),
          field({ name: MissionDocuments.fieldName.uploadedBy, label: 'Uploaded by', cols: 6 }),
          field({ name: MissionDocuments.fieldName.uploadedAt, label: 'Uploaded at', cols: 6 }),
          field({ name: MissionDocuments.fieldName.digest, label: 'Digest', cols: 6 }),
          field({ name: MissionDocuments.fieldName.provenance, label: 'Provenance', cols: 6 }),
        ],
      }),
    ],
  }),
});
