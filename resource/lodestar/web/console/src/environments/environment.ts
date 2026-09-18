// This file can be replaced during build by using the `fileReplacements` array.
// `ng build` replaces `environment.ts` with `environment.prod.ts`.

export const environment = {
  production: false,
  baseUrl: '',
  apiUrl: '/api',
  // The idle session in seconds, provided to the library's tokens in app.config.ts. A
  // development build is short so the warning, its countdown, and the stay-logged-in
  // action can be watched: the warning after two minutes idle, the session ended a minute
  // later, and the keepalive asking the server for the session every half minute.
  idle: { sessionSeconds: 180, warningSeconds: 60, keepAliveSeconds: 30 },
};
