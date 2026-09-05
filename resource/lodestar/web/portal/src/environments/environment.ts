// The portal's API lives under the portal outlet's prefix: its permission digest and
// user-domains channels are served there too, so the generated client bootstraps
// under a non-default prefix.
export const environment = {
  production: false,
  baseUrl: '',
  apiUrl: '/portal',
};
