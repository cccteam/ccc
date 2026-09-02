import { HTTP_INTERCEPTORS, provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';
import { ApplicationConfig, importProvidersFrom } from '@angular/core';
import { MatNativeDateModule } from '@angular/material/core';
import { BrowserAnimationsModule } from '@angular/platform-browser/animations';
import { provideRouter, withComponentInputBinding, withRouterConfig } from '@angular/router';
import { methodMeta } from '@app/service/zz_gen_methods';
import { resourceMeta } from '@app/service/zz_gen_resources';
import {
  API_URL,
  BASE_URL,
  FRONTEND_LOGIN_PATH,
  METHOD_META,
  RESOURCE_META,
  SESSION_PATH,
} from '@cccteam/ccc-lib/types';
import { ApiInterceptor } from '@cccteam/ccc-lib/ui-interceptor';
import { environment } from '@env';
import { routes } from './app.routes';

export const appConfig: ApplicationConfig = {
  providers: [
    {
      provide: FRONTEND_LOGIN_PATH,
      useValue: '/login',
    },
    {
      provide: SESSION_PATH,
      useValue: 'user/session',
    },
    {
      provide: RESOURCE_META,
      useValue: resourceMeta,
    },
    {
      provide: METHOD_META,
      useValue: methodMeta,
    },
    {
      provide: HTTP_INTERCEPTORS,
      useClass: ApiInterceptor,
      multi: true,
    },
    {
      provide: BASE_URL,
      useValue: environment.baseUrl,
    },
    {
      provide: API_URL,
      useValue: environment.apiUrl,
    },
    provideRouter(
      routes,
      withComponentInputBinding(),
      withRouterConfig({
        paramsInheritanceStrategy: 'always',
      }),
    ),
    importProvidersFrom(MatNativeDateModule, BrowserAnimationsModule),
    provideHttpClient(withInterceptorsFromDi()),
  ],
};
