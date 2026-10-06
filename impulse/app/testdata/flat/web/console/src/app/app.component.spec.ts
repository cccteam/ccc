// The one spec the fixture carries: the test-runner check counts it.
// A spec pins the library's page state, whose offset field is the server's answer, not a
// request; the paging check does not read specs.
// expect(store.page()).toEqual({ rows, offset: 0, hasPrev: false, hasNext: false, total: 6 });
export const expectedPage = { rows: [], offset: 0, hasPrev: false, hasNext: false, total: 6 };
