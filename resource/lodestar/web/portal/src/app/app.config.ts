import { provideHttpClient, withInterceptors, withXsrfConfiguration } from '@angular/common/http';
import { ApplicationConfig, isDevMode } from '@angular/core';
import { provideNativeDateAdapter } from '@angular/material/core';
import { provideAnimationsAsync } from '@angular/platform-browser/animations/async';
import { provideRouter, withComponentInputBinding, withRouterConfig } from '@angular/router';
import { provideServiceWorker } from '@angular/service-worker';
import { createApi } from '@app/service/zz_gen_api';
import { methodMeta } from '@app/service/zz_gen_methods';
import { resourceMeta } from '@app/service/zz_gen_resources';
import { apiVersionInterceptor, provideResourceClient } from '@cccteam/resource-angular/resource-client';
import {
  API_URL,
  API_VERSION,
  BASE_URL,
  FRONTEND_LOGIN_PATH,
  METHOD_META,
  RESOURCE_META,
  SESSION_PATH,
} from '@cccteam/resource-angular/types';
import { provideAppUpdate } from '@cccteam/resource-angular/ui-app-update';
import { environment } from '@env';
import { routes } from './app.routes';

export const appConfig: ApplicationConfig = {
  providers: [
    { provide: FRONTEND_LOGIN_PATH, useValue: '/login' },
    { provide: SESSION_PATH, useValue: 'user/session' },
    { provide: RESOURCE_META, useValue: resourceMeta },
    { provide: METHOD_META, useValue: methodMeta },
    { provide: BASE_URL, useValue: environment.baseUrl },
    { provide: API_URL, useValue: environment.apiUrl },
    // The release this build was made from, as the build stamped it (APP_VERSION; 'dev'
    // for a local build): sent in X-Api-Version on every request, so the server can refuse
    // a portal build it no longer answers and the update service reloads onto the current
    // one. The portal outlet declares no oldest answered release, so every release that
    // sends the header is answered up to the server's own.
    //
    // Demonstrates: api.version-refusal.
    { provide: API_VERSION, useValue: APP_VERSION },
    // The generated API client: one typed surface over every route, one permission
    // cache for the app's pages and the library's guard, directive, and forms. The
    // library hands the factory the options it owns and the application spreads them
    // beside its own baseUrl: the transport over HttpClient, which counts each request
    // as activity, and the error hook, which on a 401 keeps the attempted URL in
    // AuthService.redirectUrl and returns the browser to FRONTEND_LOGIN_PATH, where the
    // login page sends it back through the directory. With the client comes the
    // ErrorHandler: an ApiError nobody caught raises one global notice in the server's
    // words, and a refusal a page reports in place raises none. The portal keeps no HTTP
    // interceptor; HttpClient carries the XSRF echo alone.
    //
    // Demonstrates: client.login-redirect, client.uncaught-notice.
    provideResourceClient((options) => createApi({ baseUrl: environment.apiUrl, ...options })),
    provideRouter(routes, withComponentInputBinding(), withRouterConfig({ paramsInheritanceStrategy: 'always' })),
    // The date adapter and the animations as standalone providers: the animations module,
    // imported through the module-to-providers bridge, carried the browser module's
    // providers, whose default ErrorHandler replaced the one provideResourceClient registers
    // above, and the adapter refuses to start that way.
    provideNativeDateAdapter(),
    provideAnimationsAsync(),
    // The XSRF cookie is the members auth's (pkg/auth/members, XSRFCookie): HttpClient echoes it in
    // the X-XSRF-TOKEN header on every mutating request, and the server verifies the echo.
    // The one interceptor adds the release header to the portal's own same-origin
    // HttpClient calls; it judges nothing.
    provideHttpClient(withInterceptors([apiVersionInterceptor]), withXsrfConfiguration({ cookieName: 'members-xsrf' })),
    // The portal installs as a progressive web app, as the console does: the service
    // worker from the workspace's one ngsw-config.json, whose patterns the build joins with
    // this app's base href, so the worker's scope is /portal/ and its API exclusion is
    // /portal/api, which keeps the directory login and its callback reaching the server;
    // off in dev mode and in specs; and the library's update notice beside it.
    //
    // Demonstrates: webapp.installable, webapp.update-notice.
    provideServiceWorker('ngsw-worker.js', { enabled: !isDevMode(), registrationStrategy: 'registerWhenStable:30000' }),
    provideAppUpdate(),
  ],
};
