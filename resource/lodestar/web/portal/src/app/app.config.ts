import { provideHttpClient, withXsrfConfiguration } from '@angular/common/http';
import { ApplicationConfig } from '@angular/core';
import { provideNativeDateAdapter } from '@angular/material/core';
import { provideAnimationsAsync } from '@angular/platform-browser/animations/async';
import { provideRouter, withComponentInputBinding, withRouterConfig } from '@angular/router';
import { createApi } from '@app/service/zz_gen_api';
import { methodMeta } from '@app/service/zz_gen_methods';
import { resourceMeta } from '@app/service/zz_gen_resources';
import { provideResourceClient } from '@cccteam/resource-angular/resource-client';
import {
  API_URL,
  BASE_URL,
  FRONTEND_LOGIN_PATH,
  METHOD_META,
  RESOURCE_META,
  SESSION_PATH,
} from '@cccteam/resource-angular/types';
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
    provideHttpClient(withXsrfConfiguration({ cookieName: 'members-xsrf' })),
  ],
};
