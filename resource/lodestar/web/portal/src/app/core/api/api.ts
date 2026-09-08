import { inject } from '@angular/core';
import { Api } from '@app/service/zz_gen_api';
import { RESOURCE_CLIENT } from '@cccteam/resource-angular/resource-client';

/** The portal's typed API client, narrowed from @cccteam/resource-angular's RESOURCE_CLIENT to the generated Api. */
export function injectApi(): Api {
  return inject(RESOURCE_CLIENT) as Api;
}
