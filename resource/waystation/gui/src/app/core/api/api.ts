import { inject } from '@angular/core';
import { Api } from '@app/service/zz_gen_api';
import { RESOURCE_CLIENT } from '@cccteam/ccc-lib/resource-client';

/**
 * The application's typed API client, as registered in app.config.ts. ccc-lib holds
 * it under RESOURCE_CLIENT as the framework-neutral base type; this narrows it to the
 * generated Api so pages get the typed handles.
 */
export function injectApi(): Api {
  return inject(RESOURCE_CLIENT) as Api;
}
