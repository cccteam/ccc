import {
  HTTP_INTERCEPTORS,
  provideHttpClient,
  withInterceptorsFromDi,
  withXsrfConfiguration,
} from '@angular/common/http';
import { ApplicationConfig, computed, importProvidersFrom, inject, Injector, Signal } from '@angular/core';
import { MatNativeDateModule } from '@angular/material/core';
import { BrowserAnimationsModule } from '@angular/platform-browser/animations';
import { provideRouter, withComponentInputBinding, withRouterConfig } from '@angular/router';
import { createApi } from '@app/service/zz_gen_api';
import { methodMeta } from '@app/service/zz_gen_methods';
import { resourceMeta } from '@app/service/zz_gen_resources';
import { Domain } from '@cccteam/resource';
import { provideResourceClient } from '@cccteam/resource-angular/resource-client';
import {
  API_URL,
  BASE_URL,
  FRONTEND_LOGIN_PATH,
  METHOD_META,
  RESOURCE_DOMAIN,
  RESOURCE_META,
  SESSION_PATH,
} from '@cccteam/resource-angular/types';
import { ApiInterceptor } from '@cccteam/resource-angular/ui-interceptor';
import { SectorService } from '@components/sector/sector.service';
import { environment } from '@env';
import { routes } from './app.routes';

export const appConfig: ApplicationConfig = {
  providers: [
    { provide: FRONTEND_LOGIN_PATH, useValue: '/login' },
    { provide: SESSION_PATH, useValue: 'user/session' },
    { provide: RESOURCE_META, useValue: resourceMeta },
    { provide: METHOD_META, useValue: methodMeta },
    // The selected sector is the tenant the library's config-driven pages work in: the
    // Missions page and its pickers ask the sector's digest and bind their requests to
    // it, exactly as the hand-written decks do through SectorService. The service is
    // resolved when the signal is first read, not when the token is built: the
    // library's AuthService injects the token, and SectorService injects AuthService.
    {
      provide: RESOURCE_DOMAIN,
      useFactory: (): Signal<Domain | undefined> => {
        const injector = inject(Injector);
        return computed(() => (injector.get(SectorService).current() || undefined) as Domain | undefined);
      },
    },
    { provide: HTTP_INTERCEPTORS, useClass: ApiInterceptor, multi: true },
    { provide: BASE_URL, useValue: environment.baseUrl },
    { provide: API_URL, useValue: environment.apiUrl },
    // The generated API client: one typed surface over every route, one permission
    // cache for the app's pages and the library's guard, directive, and forms. The
    // transport rides HttpClient so the interceptor keeps applying.
    provideResourceClient((transport) => createApi({ baseUrl: environment.apiUrl, transport })),
    provideRouter(routes, withComponentInputBinding(), withRouterConfig({ paramsInheritanceStrategy: 'always' })),
    importProvidersFrom(MatNativeDateModule, BrowserAnimationsModule),
    // The XSRF cookie is the crew auth's (pkg/auth/crew, XSRFCookie): HttpClient echoes it in
    // the X-XSRF-TOKEN header on every mutating request, and the server verifies the echo.
    provideHttpClient(withInterceptorsFromDi(), withXsrfConfiguration({ cookieName: 'crew-xsrf' })),
  ],
};
