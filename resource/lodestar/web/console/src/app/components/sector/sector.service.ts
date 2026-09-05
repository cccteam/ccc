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
import { AuthService } from '@cccteam/ccc-lib/auth-service';
import { storeSignal } from '@cccteam/ccc-lib/resource-client';
import { Domain, DomainClient, Listable, ListQuery, Method, Permission, Resource, ResourceHandle } from '@cccteam/resource';

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
 * selected sector — and binds the generated API client to it. The sector is the
 * permission domain for every request those decks make: `sector()` is the client
 * bound to it, so switching sectors re-scopes what each persona can see and do.
 *
 * Permissions: the client owns the digest cache — ccc-lib's AuthService, guard, and
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
  // as the clickable path to fail-closed refusals — pick a dark sector and every
  // request on the page refuses.
  readonly chartAll = signal(false);

  // The chart has no bespoke endpoint: "where do I hold grants" is the generated
  // user-domains endpoint (AuthService.domains()), and "what does the whole frontier
  // look like" is the generated, permission-checked Sectors resource, fetched only
  // while chartAll is on and the List grant is held.
  private roster = resource({
    params: () => ({ wanted: this.chartAll() && this.can(Permissions.List, Resources.Sectors) }),
    loader: ({ params }) => (params.wanted ? this.api.sectors.list() : Promise.resolve([])),
    defaultValue: [],
  });

  /** The sectors the chart offers: lit ones, or every sector when chartAll is on. */
  readonly sectors = computed<string[]>(() => {
    if (this.chartAll()) {
      return this.roster
        .value()
        .map((row) => row.id)
        .sort();
    }

    return [...this.auth.domains()];
  });

  /** The lit sectors: where the session user holds at least one grant. */
  readonly lit = computed<readonly string[]>(() => this.auth.domains());

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
    this.permissions();
    return this.api.can(permission, target, (this.current() || undefined) as Domain | undefined);
  }

  /** The digest state — granted, conditional, or undefined — for the service card. */
  state(permission: Permission, target: Resource | Method): 'granted' | 'conditional' | undefined {
    this.permissions();
    const current = this.current();
    const scope = this.api.descriptor.resources[target]?.scope ?? this.api.descriptor.methods[target]?.scope;
    const domain = scope === 'domain' ? ((current || undefined) as Domain | undefined) : undefined;
    if (scope === 'domain' && !domain) return undefined;
    return this.api.permissions.state({ resource: target, permission, domain });
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
   * sectorList derives a deck's list from the selected sector: `select` picks the
   * handle off the sector-bound client, the loader re-runs when the sector changes,
   * sits idle while none is selected, and never asks for a list the digest says the
   * user cannot read. After a mutation, call .reload() on the affected lists.
   */
  sectorList<Row>(select: (sector: SectorApi) => ListHandle<Row>, query?: ListQuery<Row>): ResourceRef<Row[]> {
    return resource({
      params: () => {
        this.permissions();
        const sector = this.sector();
        const handle = sector ? select(sector) : undefined;
        return { handle: handle?.can(Permissions.List) ? handle : undefined };
      },
      loader: ({ params }) => (params.handle ? params.handle.list(query) : Promise.resolve([])),
      defaultValue: [],
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
}
