export const environment = {
  production: true,
  baseUrl: '/',
  apiUrl: '/api',
  // The idle session in seconds, provided to the library's tokens in app.config.ts: the
  // served build matches the server's default session timeout (APP_DEFAULT_SESSION_TIMEOUT,
  // ten minutes), warns a minute before it, and asks the server for the session every half
  // minute.
  idle: { sessionSeconds: 600, warningSeconds: 60, keepAliveSeconds: 30 },
};
