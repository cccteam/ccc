// A fixture for the paging check: a browser request that still assembles an offset.
export function secondPage(): URLSearchParams {
  const params = new URLSearchParams({ limit: '50' });
  params.set('offset', '50');
  return params;
}
