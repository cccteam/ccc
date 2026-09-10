import {
  computed,
  effect,
  inject,
  Injectable,
  linkedSignal,
  resource,
  ResourceRef,
  signal,
  untracked,
} from '@angular/core';
import { injectApi } from '@app/api/api';
import { Permissions, Resources } from '@app/service/zz_gen_constants';
import { Api, DomainApi } from '@app/service/zz_gen_api';
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
  ResourceHandle,
} from '@cccteam/resource';

/** The generated client bound to one sector: its resources and RPC methods. */
export type SectorApi = DomainClient<DomainApi>;

/** A handle a page can list from and ask about: what sectorList/globalList need. */
type ListHandle<Row> = Listable<Row> & { can(permission: Permission): boolean };

/**
 * ShipsLogEntry is one change-tracking event from the hand-written
 * /api/sectors/{sectorID}/ships-log-entries surface. The resource is registered
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
 * SectorService holds the one piece of state the sector-scoped decks share — the
 * selected sector — and binds the generated API client to it.
 *
 * Demonstrates: tenancy.user-domains, tenancy.concealed, star-chart, permission-digest, paging.descriptor-sizes.
 *
 * The rest of the class comment describes the rules the decks follow. The sector is the
 * permission domain for every request those decks make: `sector()` is the client
 * bound to it, so switching sectors re-scopes what each persona can see and do.
 *
 * Permissions: the client owns the digest cache — @cccteam/resource-angular's AuthService, guard, and
 * directive answer from the same cache. Selecting a sector loads that sector's digest;
 * `can` answers from it reactively. Absent grant = hide the surface, never provoke a
 * 403 (waystation's rule); conditional grant = render, the server narrows per row.
 *
 * Data loading is DECLARATIVE: each deck's lists are resources derived from the
 * selected sector and the List grant, never HTTP issued from an effect (an effect
 * that fetches adopts the interceptor's loading signal as a dependency and loops).
 */
@Injectable({ providedIn: 'root' })
export class SectorService {
  /** The generated client: global handles on the root, sector handles under domain(). */
  readonly api: Api = injectApi();

  private auth = inject(AuthService);

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
  // as the clickable path to fail-closed refusals: pick a dark sector and every deck
  // issues its request anyway and shows the refusal the domain guard answers.
  readonly chartAll = signal(false);

  // The chart has no bespoke endpoint: "where do I hold grants" is the generated
  // user-domains endpoint (AuthService.domains()), and "what does the whole frontier
  // look like" is the generated, permission-checked Sectors resource, fetched only
  // while chartAll is on and the GLOBAL List grant is held. The request depends on the
  // toggle and the global digest alone, never on the selected sector: round 2 derived
  // it from the selection, so selecting a dark star reloaded the roster, the roster's
  // empty loading value snapped the selection back, and the click was discarded.
  private roster = resource({
    params: () => {
      this.permissions();
      return { wanted: this.chartAll() && this.api.can(Permissions.List, Resources.Sectors) };
    },
    loader: ({ params }) => (params.wanted ? this.api.sectors.all() : Promise.resolve([])),
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

  /**
   * dark is true while the selected sector is one the session holds no grant in: the
   * digest for it is empty, so every deck would hide itself and the viewer would never
   * see the server refuse. While dark, the decks bypass the digest gate on purpose
   * (the one labelled exception to the never-provoke-a-refusal rule) and render the
   * 404 the concealed-domain guard answers.
   */
  readonly dark = computed<boolean>(() => {
    const current = this.current();
    return !!current && !this.lit().includes(current);
  });

  // current keeps the user's choice while the chart still offers it and snaps to
  // the first offered sector when the list changes underneath it — a persona
  // switch, or turning chartAll off while a dark sector is selected.
  readonly current = linkedSignal<string[], string>({
    source: this.sectors,
    computation: (sectors, previous) =>
      previous !== undefined && sectors.includes(previous.value) ? previous.value : (sectors[0] ?? ''),
  });

  /** The client bound to the selected sector; undefined while none is selected. */
  readonly sector = computed<SectorApi | undefined>(() => {
    const current = this.current();
    return current ? this.api.domain(current) : undefined;
  });

  constructor() {
    // Selecting a sector re-scopes every permission question, so load that sector's
    // digest into the client's cache. The load runs untracked (see the class comment).
    // A failed load caches an empty digest: every question about the sector answers
    // false.
    effect(() => {
      const sector = this.current();
      if (!sector) return;
      untracked(() => void this.api.permissions.loadDigest(sector as Domain).catch(() => undefined));
    });
  }

  /** The selected sector's client, for handlers that run only while one is selected. */
  sectorApi(): SectorApi {
    const sector = this.sector();
    if (!sector) {
      throw new Error('no sector selected');
    }
    return sector;
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
    // in the selected sector's digest first and the global digest second. A grant sits
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

  select(sector: string): void {
    this.current.set(sector);
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
   * carries: the status and the server's message. Only the labelled dark-sector bypass
   * ever fills it, since every other request is gated on the digest first.
   */
  refusalOf(ref: ResourceRef<unknown>): string | undefined {
    const err = ref.error();
    if (!err) return undefined;
    if (err instanceof ApiError) return `${err.status}: ${err.message}`;
    return err instanceof Error ? err.message : String(err);
  }

  /**
   * sectorList derives a deck's list from the selected sector: `select` picks the
   * handle off the sector-bound client, the loader re-runs when the sector changes,
   * sits idle while none is selected, and never asks for a list the digest says the
   * user cannot read, except while the selected sector is dark, when it asks anyway so
   * the refusal is seen. After a mutation, call .reload() on the affected lists.
   */
  sectorList<Row>(select: (sector: SectorApi) => ListHandle<Row>, query?: ListQuery<Row>): ResourceRef<Row[]> {
    return resource({
      params: () => {
        this.permissions();
        const sector = this.sector();
        const handle = sector ? select(sector) : undefined;
        return { handle: handle && (handle.can(Permissions.List) || this.dark()) ? handle : undefined };
      },
      loader: ({ params }) => (params.handle ? params.handle.list(query) : Promise.resolve([])),
      defaultValue: [],
    });
  }

  /**
   * sectorAll is sectorList over every row: the client's all() asks limit=all where the
   * resource declares no maximum and walks the pages to the end otherwise, so an
   * export sees each row once.
   */
  sectorAll<Row>(select: (sector: SectorApi) => ListHandle<Row>, query?: ListQuery<Row>): ResourceRef<Row[]> {
    return resource({
      params: () => {
        this.permissions();
        const sector = this.sector();
        const handle = sector ? select(sector) : undefined;
        return { handle: handle && (handle.can(Permissions.List) || this.dark()) ? handle : undefined };
      },
      loader: ({ params }) => (params.handle ? params.handle.all(query) : Promise.resolve([])),
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
        return { handle: handle && (handle.can(Permissions.List) || this.dark()) ? handle : undefined };
      },
      loader: ({ params }) => (params.handle ? params.handle.page(query) : Promise.resolve(undefined)),
    });
  }

  /** globalList is sectorList's global-resource sibling: no sector involved, same List gate. */
  globalList<Row>(select: (api: Api) => ListHandle<Row>, query?: ListQuery<Row>): ResourceRef<Row[]> {
    return resource({
      params: () => {
        this.permissions();
        const handle = select(this.api);
        return { handle: handle.can(Permissions.List) ? handle : undefined };
      },
      loader: ({ params }) => (params.handle ? params.handle.list(query) : Promise.resolve([])),
      defaultValue: [],
    });
  }

  /** globalAll is globalList over every row, through the client's all(). */
  globalAll<Row>(select: (api: Api) => ListHandle<Row>, query?: ListQuery<Row>): ResourceRef<Row[]> {
    return resource({
      params: () => {
        this.permissions();
        const handle = select(this.api);
        return { handle: handle.can(Permissions.List) ? handle : undefined };
      },
      loader: ({ params }) => (params.handle ? params.handle.all(query) : Promise.resolve([])),
      defaultValue: [],
    });
  }

  /** globalPage is sectorPage's global-resource sibling. */
  globalPage<Row>(select: (api: Api) => ListHandle<Row>, query?: ListQuery<Row>): ResourceRef<Page<Row> | undefined> {
    return resource({
      params: () => {
        this.permissions();
        const handle = select(this.api);
        return { handle: handle.can(Permissions.List) ? handle : undefined };
      },
      loader: ({ params }) => (params.handle ? params.handle.page(query) : Promise.resolve(undefined)),
    });
  }
}
