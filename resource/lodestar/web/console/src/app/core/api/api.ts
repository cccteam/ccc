import { inject } from '@angular/core';
import { Api } from '@app/service/zz_gen_api';
import { RESOURCE_CLIENT } from '@cccteam/resource-angular/resource-client';

/**
 * The console's typed API client, as registered in app.config.ts. @cccteam/resource-angular holds it
 * under RESOURCE_CLIENT as the framework-neutral base type; this narrows it to the
 * generated Api so pages get the typed handles.
 */
export function injectApi(): Api {
  return inject(RESOURCE_CLIENT) as Api;
}
