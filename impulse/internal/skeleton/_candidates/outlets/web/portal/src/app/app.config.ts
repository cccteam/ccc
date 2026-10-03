import { provideHttpClient, withXsrfConfiguration } from '@angular/common/http';
import { ApplicationConfig, isDevMode } from '@angular/core';
import { provideNativeDateAdapter } from '@angular/material/core';
import { provideAnimationsAsync } from '@angular/platform-browser/animations/async';
import { provideRouter, withComponentInputBinding, withRouterConfig } from '@angular/router';
import { provideServiceWorker } from '@angular/service-worker';
import { createApi } from '@app/service/zz_gen_api';
import { methodMeta } from '@app/service/zz_gen_methods';
import { resourceMeta } from '@app/service/zz_gen_resources';
import { provideResourceClient } from '@cccteam/resource-angular/resource-client';
import {
  API_URL,
  BASE_URL,
  CHANGE_FEED,
  FRONTEND_LOGIN_PATH,
  METHOD_META,
  RESOURCE_META,
  SESSION_PATH,
} from '@cccteam/resource-angular/types';
import { provideAppUpdate } from '@cccteam/resource-angular/ui-app-update';
import { firestoreChangeFeed } from '@cccteam/resource-firestore';
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
    // The generated API client: one typed surface over every route, one permission
    // cache for the app's pages and the library's guard, directive, and forms. The
    // library hands the factory the options it owns and the application spreads them
    // beside its own baseUrl: the transport over HttpClient, which counts each request
    // as activity, and the error hook, which on a 401 returns the browser to the login
    // page with the attempted URL kept. With the client comes the ErrorHandler: an
    // ApiError nobody caught raises one global notice in the server's words, and a
    // refusal a page reports in place raises none. No HTTP interceptor; HttpClient
    // carries the XSRF echo alone.
    provideResourceClient((options) => createApi({ baseUrl: environment.apiUrl, ...options })),
    // The change feed the live pages listen through: the Firestore feed from its own
    // package, so the client stays free of the SDK. AuthService starts the client's live
    // session with it once the session is authenticated, fetching the tab's identity from
    // the API's token route (which says whether to connect to the emulator or to sign a
    // custom token in), and stops it at logout, unsubscribing everything and revoking the
    // identity before the session itself is logged out. Nothing Firestore-specific lives in
    // the environment: the project, the database, the emulator host and the key all arrive
    // in the token payload. A page is live only when its view configuration says so
    // (live: true on a list or record page); the provider alone makes nothing live.
    { provide: CHANGE_FEED, useFactory: () => firestoreChangeFeed() },
    provideRouter(routes, withComponentInputBinding(), withRouterConfig({ paramsInheritanceStrategy: 'always' })),
    // The date adapter and the animations as standalone providers: the animations module,
    // imported through the module-to-providers bridge, carried the browser module's
    // providers, whose default ErrorHandler replaced the one provideResourceClient registers
    // above, and the adapter refuses to start that way.
    provideNativeDateAdapter(),
    provideAnimationsAsync(),
    // The XSRF cookie is the members auth's (pkg/auth/members, XSRFCookie): HttpClient echoes it in
    // the X-XSRF-TOKEN header on every mutating request, and the server verifies the echo.
    provideHttpClient(withXsrfConfiguration({ cookieName: 'members-xsrf' })),
    // The service worker that installs the application and keeps an open tab on the build
    // it loaded through a release: the worker keeps that build's files, so a lazy chunk
    // still loads after a deploy, and the new build is picked up when the person chooses.
    // Off in dev mode, where the dev server emits no worker; registered once the
    // application is stable or thirty seconds after start, whichever comes first. The
    // update provider starts the library's update service: one persistent notice with
    // Reload when a new build is ready, and the server's build picked up when the server
    // refuses this one's release.
    provideServiceWorker('ngsw-worker.js', { enabled: !isDevMode(), registrationStrategy: 'registerWhenStable:30000' }),
    provideAppUpdate(),
  ],
};
