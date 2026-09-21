// Demonstrates: list.keyless.
import { ApplicationRef } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ActivatedRoute } from '@angular/router';
import { createApi } from '@app/service/zz_gen_api';
import { resourceMeta } from '@app/service/zz_gen_resources';
import { TransportRequest, TransportResponse } from '@cccteam/resource';
import { CompoundResourceComponent, ResourceListComponent } from '@cccteam/resource-angular/ccc-resource';
import { resourceRoutes } from '@cccteam/resource-angular/resource-route-generator';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { ListViewConfig, RESOURCE_META, rootConfig } from '@cccteam/resource-angular/types';
import { ScriptedTransport, scriptedTransport } from '@cccteam/resource/testing';
import { environment } from '@env';
import { standingOrdersConfig } from './standingOrders.config';

// The Standing Orders page is the library's list over a key-less resource. The server as
// the spec plays it answers the whole book in one response with its count, as the real
// one does for a bare GET; the page draws every line as one page, identified by
// position, with Previous and Next disabled, no View column, and no row route, and a
// config asking for a page size or a row expansion fails when the page is built naming
// StandingOrders and the reason.

/** The book, in its own order: section by section, neither alphabetical nor keyed. */
const book = [
  { section: 'General', directive: "The sector marshal's word is final while a mission is underway." },
  { section: 'General', directive: 'Every hail is answered, and every distress call is logged before it is judged.' },
  { section: 'Flight', directive: 'No launch without a filed briefing sheet and a named flight lead.' },
  { section: 'Flight', directive: 'A hazard-4 lane is flown by a certified pilot or not at all.' },
  { section: 'Hangar', directive: 'A hull leaves the quarantine bay only after its inspection is signed.' },
  { section: 'Salvage', directive: 'Bonded cargo is released once, to the consignee, against the bond.' },
];

function server(request: TransportRequest): TransportResponse {
  const url = new URL(request.url, 'http://lodestar.test');
  if (url.pathname === `${environment.apiUrl}/standing-orders`) {
    return { status: 200, body: book, headers: { 'total-count': String(book.length) } };
  }
  return { status: 404, body: { message: `unscripted ${request.method} ${request.url}` } };
}

describe('the Standing Orders page', () => {
  let fixture: ComponentFixture<ResourceListComponent>;
  let transport: ScriptedTransport;

  /** Builds the list component the page's route loads, over the console's generated client. */
  const create = async (config: ListViewConfig): Promise<void> => {
    transport = scriptedTransport(server);
    await TestBed.configureTestingModule({
      imports: [ResourceListComponent],
      providers: [
        provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
        { provide: RESOURCE_META, useValue: resourceMeta },
        {
          provide: ActivatedRoute,
          useValue: { snapshot: { data: { config: rootConfig({ ...standingOrdersConfig, parentConfig: config }) }, params: {}, queryParams: {} } },
        },
      ],
    }).compileComponents();
    fixture = TestBed.createComponent(ResourceListComponent);
    fixture.componentRef.setInput('compoundResourceComponent', CompoundResourceComponent);
    fixture.componentRef.setInput('resourceConfig', config);
  };

  const page = (): ListViewConfig => standingOrdersConfig.parentConfig as ListViewConfig;

  /** Waits for the store's page request, a plain promise the application's stability does not track, to land. */
  const settle = async (): Promise<void> => {
    await TestBed.inject(ApplicationRef).whenStable();
    for (let i = 0; i < 50 && fixture.componentInstance.store.pageStatus() !== 'resolved'; i++) {
      await new Promise((resolve) => setTimeout(resolve, 0));
    }
    fixture.detectChanges();
  };

  describe('as configured', () => {
    beforeEach(async () => {
      await create(page());
      fixture.detectChanges();
      await settle();
    });

    it('holds the whole book as its one page', () => {
      const store = fixture.componentInstance.store;
      expect(store.pageError()).toBeUndefined();
      expect(store.pageStatus()).toBe('resolved');
      expect(store.page()).toEqual({ rows: book, offset: 0, hasPrev: false, hasNext: false, total: 6 });
    });

    it('asks for the whole book once, with no limit and no cursor', () => {
      expect(transport.requests.map((request) => `${request.method} ${request.url}`)).toEqual([
        `GET ${environment.apiUrl}/standing-orders?columns=section%2Cdirective&count=true`,
      ]);
    });

    it('draws the six lines as one page, in the book\'s order, with the turns disabled', () => {
      const element: HTMLElement = fixture.nativeElement;
      const rows = Array.from(element.querySelectorAll('tr.ccc-row'));
      expect(rows.map((row) => row.querySelector('td.data-col')?.textContent?.trim())).toEqual(book.map((line) => line.section));
      expect(element.querySelector('.ccc-grid-pager .pager-rows')?.textContent?.trim()).toBe('1–6 of 6');
      const turns = Array.from(element.querySelectorAll<HTMLButtonElement>('.ccc-grid-pager .pager-buttons button'));
      expect(turns.length).toBe(3);
      expect(turns.every((button) => button.disabled)).toBe(true);
    });

    it('draws no View column, no create, and opens nothing', () => {
      const element: HTMLElement = fixture.nativeElement;
      expect(element.querySelectorAll('td.action-col').length).toBe(0);
      expect(element.querySelectorAll('th.data-col').length).toBe(2);
      expect(Array.from(element.querySelectorAll('th .col-header')).map((th) => th.textContent?.trim())).toEqual(['Section', 'Directive']);
      expect(element.querySelector('.create-button')).toBeNull();
      const component = fixture.componentInstance;
      expect(component.keyless()).toBe(true);
      expect(component.rowKey()).toBeUndefined();
      expect(component.rowTarget()).toBeUndefined();
      expect(component.canView()).toBe(false);
    });
  });

  it('has a list route and no row route', () => {
    const route = resourceRoutes(standingOrdersConfig, resourceMeta);
    expect(route.path).toBe('standing-orders');
    expect(route.children?.map((child) => child.path)).toEqual(['']);
  });

  describe('refuses a config asking for what the page cannot have', () => {
    const cases: { name: string; config: ListViewConfig; want: RegExp }[] = [
      {
        name: 'a page size: the book is served whole',
        config: { ...page(), pageSize: 10 },
        want: /^StandingOrders: pageSize 10 is set, .* served whole, no page size$/,
      },
      {
        name: 'a row expansion: no key to open a line by',
        config: { ...page(), enableVirtualScroll: false, enableRowExpansion: true },
        want: /^StandingOrders: enableRowExpansion is set, .* no key to open a row by$/,
      },
    ];

    for (const tt of cases) {
      it(tt.name, async () => {
        await create(tt.config);
        expect(() => fixture.detectChanges()).toThrow(tt.want);
        expect(transport.requests).toEqual([]);
      });
    }
  });
});
