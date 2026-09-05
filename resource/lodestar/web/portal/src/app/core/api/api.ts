import { inject } from '@angular/core';
import { Api } from '@app/service/zz_gen_api';
import { RESOURCE_CLIENT } from '@cccteam/ccc-lib/resource-client';

/** The portal's typed API client, narrowed from ccc-lib's RESOURCE_CLIENT to the generated Api. */
export function injectApi(): Api {
  return inject(RESOURCE_CLIENT) as Api;
}
