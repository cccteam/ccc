import {
  computed,
  effect,
  inject,
  Injectable,
  linkedSignal,
  resource,
  ResourceRef,
  Signal,
  signal,
  untracked,
} from '@angular/core';
import { PRIMARY_OUTLET, Router, UrlSegment, UrlSegmentGroup } from '@angular/router';
import { injectApi } from '@app/api/api';
import { Permissions, Resources } from '@app/service/zz_gen_constants';
import { Api, apiDescriptor, DomainApi } from '@app/service/zz_gen_api';
import { ResourceScopes } from '@app/service/zz_gen_resources';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { storeSignal } from '@cccteam/resource-angular/resource-client';
import {
  ApiError,
  Domain,
  DomainClient,
  Listable,
  ListQuery,
  Method,
  Page,
  Permission,
  Resource,
  ResourceDescriptor,
  ResourceHandle,
} from '@cccteam/resource';

/** The generated client bound to one sector: its resources and RPC methods. */
export type SectorApi = DomainClient<DomainApi>;

/** The segment under a sector where a URL naming a sector the session cannot reach lands. */
export const refusedSegment = 'refused';

/** The page a sector opens on when the star chart is used from the refusal page. */
const landing = 'dashboard';

/** A handle a page can list from and ask about: what sectorList/globalList need. */
export type ListHandle<Row> = Listable<Row> & {
  readonly descriptor: ResourceDescriptor;
  can(permission: Permission): boolean;
  grantedFields(permission: Permission): readonly string[] | undefined;
};

/**
 * The query a global reader passes: a ListQuery as written, or a function that builds one
 * from the handle (grantedColumns). The function runs inside the reader's params, so the
 * reader reads again when what it builds changes, and not when a digest reload builds the
 * same query again.
 */
export type ListQueryOf<Row> = ListQuery<Row> | ((handle: ListHandle<Row>) => ListQuery<Row>);

/**
 * grantedColumns asks for exactly the columns the digest grants on the handle's resource,
 * with its keys: the query for a reader of a resource with a field behind a feature flag
 * (the pilot card's citation count, @feature.field). The server serves a gated field only
 * when it is named in columns= and refuses the name while the flag is off, so a bare list
 * never carries it; the digest names the field exactly while the flag is on, so this query
 * follows the flag, and the person who flips it sees their own read follow at once (the
 * flip refreshes the digest). A digest that enumerates no fields for the resource asks for
 * the default columns.
 */
export function grantedColumns<Row>(handle: ListHandle<Row>): ListQuery<Row> {
  const fields = handle.grantedFields(Permissions.List);
  if (!fields) {
    return {};
  }
  return { columns: [...new Set([...handle.descriptor.keys, ...fields])] as (keyof Row & string)[] };
}

/** What a global reader reads with: the granted handle and the query it was given, built. */
interface GlobalRead<Row> {
  handle: ListHandle<Row>;
  query: ListQuery<Row> | undefined;
}

/**
 * ShipsLogEntry is one change-tracking event from the hand-written
 * /console/api/sectors/{sectorID}/ships-log-entries surface. The resource is registered
 * manually (@manualAddResource(List, domain)) so no generated row type exists; the
 * shape mirrors app.shipsLogEntry's wire struct.
 */
export interface ShipsLogEntry {
  tableName: string;
  rowId: string;
  sequence: number;
  eventTime: Date;
  eventSource: string;
  changeSet: Record<string, unknown> | null;
}

/**
 * SectorService holds the one piece of state the sector-scoped decks share — the sector
 * the deck is open in — and binds the generated API client to it.
 *
 * Demonstrates: tenancy.user-domains, tenancy.concealed, star-chart, permission-digest, paging.descriptor-sizes.
 *
 * The sector is a consequence of the route, never a choice. Every sector-scoped page lives
 * under the sector segment (`sectors/:sectorID`, the generated DomainRoutePath, mirroring
 * the API's), the sector guard on that segment writes `current` from the URL before the
 * page loads and clears it when the URL leaves the segment, and nothing else writes it.
 * The star chart sets no sector: it navigates to the same page under the chosen sector
 * (`open`) and the guard does the rest, so a link to a sector's deck can be shared and a
 * bookmark lands where it points; a sector the session cannot reach lands on the refusal
 * page, in the server's words.
 *
 * The rest of the class comment describes the rules the decks follow. The sector is the
 * permission domain for every request those decks make: `sector()` is the client
 * bound to it, so moving between sectors re-scopes what each persona can see and do.
 *
 * Permissions: the client owns the digest cache — @cccteam/resource-angular's AuthService, guard, and
 * directive answer from the same cache. Entering a sector loads that sector's digest;
 * `can` answers from it reactively. Absent grant = hide the surface, never provoke a
 * 403 (waystation's rule); conditional grant = render, the server narrows per row.
 *
 * Data loading is DECLARATIVE: each deck's lists are resources derived from the
 * open sector and the List grant, never HTTP issued from an effect (an effect
 * that fetches adopts the interceptor's loading signal as a dependency and loops).
 */
@Injectable({ providedIn: 'root' })
export class SectorService {
  /** The generated client: global handles on the root, sector handles under domain(). */
  readonly api: Api = injectApi();

  private auth = inject(AuthService);
  private router = inject(Router);

  // The digest cache mirrored into a signal: can() reads it so computeds re-evaluate
  // when a digest loads, then asks the client for the answer.
  private permissions = storeSignal(this.api.permissions.snapshot);

  /**
   * The ship's log is a manually registered, sector-scoped resource (no struct, no
   * generated handle): the client's escape hatch describes it once per sector.
   */
  shipsLog(sector: string): ResourceHandle<ShipsLogEntry, [], 'list'> {
    return this.api.define<ShipsLogEntry, [], 'list'>(
      {
        resource: Resources.ShipsLogEntries,
        property: 'shipsLogEntries',
        route: 'ships-log-entries',
        scope: 'domain',
        consolidated: false,
        keys: [],
        operations: ['list'],
      },
      sector as Domain,
    );
  }

  // chartAll widens the star chart from the permission-derived constellation to the
  // full roster of sectors. A real application would not offer it; the demo keeps it
  // as the clickable path to a concealed refusal: open a dark sector and the sector
  // guard lands on the refusal page, which shows the 404 the domain guard answers.
  readonly chartAll = signal(false);

  // The chart has no bespoke endpoint: "where do I hold grants" is the generated
  // user-domains endpoint (AuthService.domains()), and "what does the whole frontier
  // look like" is the generated, permission-checked Sectors resource, fetched only
  // while chartAll is on and the GLOBAL List grant is held. The request depends on the
  // toggle and the global digest alone, never on the open sector: round 2 derived
  // it from the selection, so selecting a dark star reloaded the roster, the roster's
  // empty loading value snapped the selection back, and the click was discarded.
  // Sectors declares a maximum page size, so it is never read whole: the chart asks for
  // one page of the descriptor's maximum, the most one request may carry, and nothing
  // here walks past it.
  private roster = resource({
    params: () => {
      this.permissions();
      return { wanted: this.chartAll() && this.api.can(Permissions.List, Resources.Sectors) };
    },
    loader: ({ params }) =>
      params.wanted ? this.api.sectors.list({ limit: this.api.sectors.descriptor.page?.max }) : Promise.resolve([]),
    defaultValue: [],
  });

  // The chart holds the last roster while the next one loads, so a reload never
  // empties the constellation under a selection.
  private heldRoster = linkedSignal<string[] | undefined, string[]>({
    source: () =>
      this.roster.hasValue() && this.roster.value().length
        ? this.roster
            .value()
            .map((row) => row.id)
            .sort()
        : undefined,
    computation: (roster, previous) => roster ?? previous?.value ?? [],
  });

  /** The sectors the chart offers: lit ones, or every sector when chartAll is on. */
  readonly sectors = computed<string[]>(() => {
    if (this.chartAll()) {
      const held = this.heldRoster();
      return held.length ? held : [...this.auth.domains()];
    }

    return [...this.auth.domains()];
  });

  /** The lit sectors: where the session user holds at least one grant. */
  readonly lit = computed<readonly string[]>(() => this.auth.domains());

  // The sector the URL names: the sector guard's alone to write (enter, leave).
  private routed = signal('');

  /** The sector the deck is open in; empty outside the sector segment. */
  readonly current = this.routed.asReadonly();

  /** The client bound to the open sector; undefined while none is open. */
  readonly sector = computed<SectorApi | undefined>(() => {
    const current = this.current();
    return current ? this.api.domain(current) : undefined;
  });

  constructor() {
    // Entering a sector re-scopes every permission question, so load that sector's
    // digest into the client's cache. The load runs untracked (see the class comment).
    // A failed load caches an empty digest: every question about the sector answers
    // false.
    effect(() => {
      const sector = this.current();
      if (!sector) return;
      untracked(() => void this.api.permissions.loadDigest(sector as Domain).catch(() => undefined));
    });
  }

  /** The open sector's client, for handlers that run only while one is open. */
  sectorApi(): SectorApi {
    const sector = this.sector();
    if (!sector) {
      throw new Error('no sector open');
    }
    return sector;
  }

  /**
   * enter is the sector guard's write, and nobody else's: the guard calls it with the
   * sector the URL names once the session is known to reach it, before any deck under
   * the segment loads.
   */
  enter(sector: string): void {
    this.routed.set(sector);
  }

  /** leave is the guard's other write: the URL has left the sector segment. */
  leave(): void {
    this.routed.set('');
  }

  /**
   * open navigates to the page the browser is on, under the given sector: the star
   * chart's action. A page under a sector keeps its path with the sector replaced; a page
   * outside one enters the sector on that path (`/dashboard` opens
   * `/sectors/anvil/dashboard`); the refusal page opens the sector's landing. Query and
   * fragment travel along, and the guard on the segment settles the sector from the new
   * URL — or refuses it, for a dark star.
   */
  open(sector: string): Promise<boolean> {
    const { segment } = apiDescriptor.domainRoute;
    const tree = this.router.parseUrl(this.router.url);
    const primary = tree.root.children[PRIMARY_OUTLET];
    const segments = primary?.segments ?? [];
    const underSector = segments[0]?.path === segment && segments.length > 1;
    let page = underSector ? segments.slice(2) : segments;
    if (page[0]?.path === refusedSegment) {
      page = [new UrlSegment(landing, {})];
    }
    const under = [new UrlSegment(segment, {}), new UrlSegment(sector, {}), ...page];
    if (primary) {
      primary.segments = under;
    } else {
      tree.root.children[PRIMARY_OUTLET] = new UrlSegmentGroup(under, {});
    }
    return this.router.navigateByUrl(tree);
  }

  /**
   * can answers one permission question from the digest — the app's single gate for
   * requests and affordances. Conditional grants answer true — render, and let the
   * server narrow per row. Signal-backed, so lists and buttons re-evaluate when a
   * digest loads or the sector changes.
   */
  can(permission: Permission, target: Resource | Method): boolean {
    return this.state(permission, target) !== undefined;
  }

  /** The digest state — granted, conditional, or undefined — for the service card. */
  state(permission: Permission, target: Resource | Method): 'granted' | 'conditional' | undefined {
    this.permissions();
    const sector = (this.current() || undefined) as Domain | undefined;
    const scope = this.scopeOf(target);
    if (scope === 'domain') {
      return sector ? this.api.permissions.state({ resource: target, permission, domain: sector }) : undefined;
    }
    if (scope === 'global') {
      return this.api.permissions.state({ resource: target, permission });
    }
    // A target the generated metadata does not place (the manual registrations:
    // ShipsLogEntries is sector-scoped, ViewAsUser and AssumeRole are global) is asked
    // in the open sector's digest first and the global digest second. A grant sits
    // in exactly one of them, and absence from both still fails closed.
    return (
      (sector && this.api.permissions.state({ resource: target, permission, domain: sector })) ||
      this.api.permissions.state({ resource: target, permission })
    );
  }

  /**
   * The scope the generated metadata gives a target: the API descriptor places every
   * table, virtual, and computed resource and every RPC method; the ResourceScopes map
   * repeats the resources. Manual registrations appear in neither, so undefined means
   * "unplaced", and state() asks both digests.
   */
  private scopeOf(target: Resource | Method): 'domain' | 'global' | undefined {
    return (
      this.api.descriptor.resources[target]?.scope ??
      this.api.descriptor.methods[target]?.scope ??
      ResourceScopes[target as Resource]
    );
  }

  /**
   * The digest state for one field of a domain resource under a permission: granted
   * when an unconditional grant covers the field, conditional when only a
   * condition-limited grant does (the condition's text never reaches the browser, so a
   * form can say "the server judges this" and nothing more precise), undefined when
   * no grant reaches it. Demonstrates: capability-envelope.
   */
  fieldState(permission: Permission, resource: Resource, field: string): 'granted' | 'conditional' | undefined {
    this.permissions();
    const domain = (this.current() || undefined) as Domain | undefined;
    if (!domain) return undefined;
    return this.api.permissions.fieldStates({ resource, permission, domain })[field];
  }

  /**
   * grantedFields is the digest's field-level enumeration for a resource — for Create,
   * the inputs a form is worth rendering. Undefined means the digest carries no
   * field-level entries for the permission, so narrow only on a defined answer.
   */
  grantedFields(permission: Permission, resource: Resource): readonly string[] | undefined {
    this.permissions();
    return this.api.grantedFields(permission, resource, (this.current() || undefined) as Domain | undefined);
  }

  setChartAll(all: boolean): void {
    this.chartAll.set(all);
  }

  /**
   * pageSize is the resource's declared default page, from the generated descriptor:
   * change the @page annotation, regenerate, and the deck resizes. No page size is a
   * literal in this application.
   */
  pageSize(target: Resource): number | undefined {
    return this.api.descriptor.resources[target]?.page?.default;
  }

  /**
   * refusalOf renders the refusal a deck's list met, for the refusal slot every deck
   * carries: the status and the server's message. Every request is gated on the digest
   * first, so the slot fills only when the server refuses what the digest allowed.
   */
  refusalOf(ref: ResourceRef<unknown>): string | undefined {
    const err = ref.error();
    if (!err) return undefined;
    if (err instanceof ApiError) return `${err.status}: ${err.message}`;
    return err instanceof Error ? err.message : String(err);
  }

  /**
   * sectorList derives a deck's list from the open sector: `select` picks the
   * handle off the sector-bound client, the loader re-runs when the sector changes,
   * sits idle while none is open, and never asks for a list the digest says the
   * user cannot read. After a mutation, call .reload() on the affected lists. A deck
   * that wants every row of a resource with no maximum page size passes
   * `{ limit: 'all' }`; a resource with a maximum is read a page at a time (sectorPage).
   */
  sectorList<Row>(select: (sector: SectorApi) => ListHandle<Row>, query?: ListQuery<Row>): ResourceRef<Row[]> {
    return resource({
      params: () => {
        this.permissions();
        const sector = this.sector();
        const handle = sector ? select(sector) : undefined;
        return { handle: handle && handle.can(Permissions.List) ? handle : undefined };
      },
      loader: ({ params }) => (params.handle ? params.handle.list(query) : Promise.resolve([])),
      defaultValue: [],
    });
  }

  /**
   * sectorPage is sectorList's paged form: the loader asks the server for one page
   * with its neighbors (the resource's declared default size when the query names
   * none), and the component steps through them by setting the resource to
   * page.next() or page.prev(), so every page is the server's own answer, positioned
   * by the cursor it issued.
   */
  sectorPage<Row>(
    select: (sector: SectorApi) => ListHandle<Row>,
    query?: ListQuery<Row>,
  ): ResourceRef<Page<Row> | undefined> {
    return resource({
      params: () => {
        this.permissions();
        const sector = this.sector();
        const handle = sector ? select(sector) : undefined;
        return { handle: handle && handle.can(Permissions.List) ? handle : undefined };
      },
      loader: ({ params }) => (params.handle ? params.handle.page(query) : Promise.resolve(undefined)),
    });
  }

  /**
   * globalList is sectorList's global-resource sibling: no sector involved, same List gate.
   * The params are the handle with the query while the grant holds, and undefined
   * otherwise, when the resource idles at its default; they are a computed that counts the
   * same handle with the same query as the same value, since resource() re-runs its loader
   * whenever the params function's dependencies change, and only a computed holds its
   * consumers still while its value is the same. So a digest load that changes no answer
   * to the global question (the open sector's digest landing after sign-in, a sector
   * change) reads nothing, and the list is read once per answer, not once per digest. A
   * query given as a function (grantedColumns) is built inside the computed, so a digest
   * that changes what it builds is a new answer and reads again.
   */
  globalList<Row>(select: (api: Api) => ListHandle<Row>, query?: ListQueryOf<Row>): ResourceRef<Row[]> {
    return resource({
      params: this.grantedGlobal(select, query),
      loader: ({ params }) => params.handle.list(params.query),
      defaultValue: [],
    });
  }

  /** globalPage is sectorPage's global-resource sibling, its params globalList's. */
  globalPage<Row>(select: (api: Api) => ListHandle<Row>, query?: ListQueryOf<Row>): ResourceRef<Page<Row> | undefined> {
    return resource({
      params: this.grantedGlobal(select, query),
      loader: ({ params }) => params.handle.page(params.query),
    });
  }

  /**
   * The selected global handle with its query while the digest grants List on it, else
   * undefined: a global reader's params. Equal while the handle is the same object and the
   * query reads the same, so a reload that changes neither holds the reader still.
   */
  private grantedGlobal<Row>(
    select: (api: Api) => ListHandle<Row>,
    query?: ListQueryOf<Row>,
  ): Signal<GlobalRead<Row> | undefined> {
    return computed(
      () => {
        this.permissions();
        const handle = select(this.api);
        if (!handle.can(Permissions.List)) {
          return undefined;
        }
        return { handle, query: typeof query === 'function' ? query(handle) : query };
      },
      { equal: (a, b) => a?.handle === b?.handle && JSON.stringify(a?.query) === JSON.stringify(b?.query) },
    );
  }
}
