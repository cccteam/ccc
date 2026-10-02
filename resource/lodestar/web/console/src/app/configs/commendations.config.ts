import { Commendations, Pilots, Resources } from '@app/service/zz_gen_constants';
import { enumeratedConfig, field, listViewConfig, rootConfig, section } from '@cccteam/resource-angular/types';

// Commendations is the commendations desk: a citation on a pilot's record for a sortie
// flown well, filed by headquarters and read by the crew. It is the one resource behind a
// feature flag, and this config says nothing about the flag: the generated metadata
// carries `feature: 'commendations'`, and resourceRoutes reads it, puts the library's
// featureMatch guard on the page's route (so while the flag is off a URL to the desk
// falls to the wildcard as an unknown URL does) and marks the navigation item with the
// flag (so the topbar's *cccFeature hides it). The permission digest hides the item too
// while the flag is off, since the server leaves a gated-off resource out of it. Turn the
// flag on in the header's Feature flags dialog as the adjutant and the desk is there, its
// three seeded citations newest first (the resource's @order).
//
// AwardedBy and AwardedAt are the server's (output_only): the metadata marks them read
// only, so the create form offers the pilot and the citation alone, and the row page
// shows who filed the citation and when.
//
// Demonstrates: @feature.
export const commendationsConfig = rootConfig({
  nav: { navItem: { label: 'Commendations' }, group: 'Headquarters' },
  parentConfig: listViewConfig({
    title: 'Commendations',
    createTitle: 'Commendation',
    primaryResource: Resources.Commendations,
    listColumns: [
      { id: Commendations.fieldName.pilotId, header: 'Pilot' },
      { id: Commendations.fieldName.citation },
      { id: Commendations.fieldName.awardedBy, header: 'Awarded by' },
      { id: Commendations.fieldName.awardedAt, header: 'Awarded' },
    ],
    elements: [
      section({
        label: 'Citation',
        children: [
          field({
            name: Commendations.fieldName.pilotId,
            label: 'Pilot',
            cols: 4,
            enumeratedConfig: enumeratedConfig({
              listDisplay: [Pilots.fieldName.displayName],
              viewDisplay: [Pilots.fieldName.displayName],
            }),
          }),
          field({ name: Commendations.fieldName.citation, label: 'Citation', cols: 8 }),
          field({ name: Commendations.fieldName.awardedBy, label: 'Awarded by', cols: 6 }),
          field({ name: Commendations.fieldName.awardedAt, label: 'Awarded', cols: 6 }),
        ],
      }),
    ],
  }),
});
