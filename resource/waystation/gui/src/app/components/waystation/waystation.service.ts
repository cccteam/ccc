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
import { Domain, DomainClient, Listable, ListQuery, Method, Permission, Resource } from '@cccteam/resource';

/** The generated client bound to one waystation: its resources and RPC methods. */
export type StationApi = DomainClient<DomainApi>;

/** A handle a page can list from and ask about: what stationList/globalList need. */
type ListHandle<Row> = Listable<Row> & { can(permission: Permission): boolean };

/**
 * AuditTrailEntry is one change-tracking event from the hand-written
 * /api/audit-trail-entries surface. The resource is registered manually
 * (@manualAddResource) so no generated row type exists; the shape mirrors
 * app.auditTrailEntries's wire struct.
 */
export interface AuditTrailEntry {
  tableName: string;
  rowId: string;
  sequence: number;
  eventTime: Date;
  eventSource: string;
  changeSet: Record<string, unknown> | null;
}

/**
 * WaystationService holds the one piece of state the station-scoped pages share — the
 * selected waystation — and binds the generated API client to it. The waystation is
 * the permission domain for every request those pages make: `station()` is the
 * client bound to it (its handles fill the {waystationID} segment, address the
 * consolidated mutation endpoint, and post to the Execute-gated RPC routes), so
 * switching stations re-scopes what each persona can see and do.
 *
 * Permissions: the client owns the digest cache — ccc-lib's AuthService, guard, and
 * directive answer from the same cache. Selecting a station loads that station's
 * digest; `can` answers from it reactively. Absent grant = hide the surface, never
 * provoke a 403; conditional grant = render, the server narrows per row.
 *
 * Data loading is DECLARATIVE: each page's lists are resources derived from the
 * selected station and the List grant, never HTTP issued from an effect. An effect
 * that fetches adopts hidden dependencies — ccc-lib's ApiInterceptor reads the global
 * loading signal synchronously on every request (UiCoreService.beginActivity), so a
 * fetching effect re-runs on every response's endActivity write: an infinite request
 * loop, as fast as the server can answer. Resource loaders run untracked by design,
 * which rules that loop out structurally.
 */
@Injectable({ providedIn: 'root' })
export class WaystationService {
  /** The generated client: global handles on the root, station handles under domain(). */
  readonly api: Api = injectApi();

  private auth = inject(AuthService);

  // The digest cache mirrored into a signal: can() reads it so computeds re-evaluate
  // when a digest loads, then asks the client for the answer.
  private permissions = storeSignal(this.api.permissions.snapshot);

  /**
   * The audit trail is a manually registered resource (no struct, no generated
   * handle): the client's escape hatch describes it once, and it gets the same
   * typed list-and-can surface as the generated resources.
   */
  readonly auditTrail = this.api.define<AuditTrailEntry, [], 'list'>({
    resource: Resources.AuditTrailEntries,
    property: 'auditTrailEntries',
    route: 'audit-trail-entries',
    scope: 'global',
    consolidated: false,
    keys: [],
    operations: ['list'],
  });

  // showAll widens the picker from the permission-derived list to the full tenant
  // roster. A real application would not offer it; the demo keeps it as the
  // clickable path to fail-closed refusals — select a station you hold no roles at
  // and every list load on the page refuses.
  readonly showAll = signal(false);

  // The picker has no bespoke endpoint: its two questions are answered by surfaces
  // the library and the application already serve. "Where do I hold domain-scoped
  // grants" is the generated user-domains endpoint, loaded once per session by the
  // client and exposed on AuthService as domains(). "What does the fleet roster look
  // like" is the generated, permission-checked Waystations resource, fetched only
  // while showAll is on and the List grant is held.
  private roster = resource({
    params: () => ({ wanted: this.showAll() && this.can(Permissions.List, Resources.Waystations) }),
    loader: ({ params }) => (params.wanted ? this.api.waystations.list() : Promise.resolve([])),
    defaultValue: [],
  });

  readonly waystations = computed<string[]>(() => {
    if (this.showAll()) {
      return this.roster
        .value()
        .map((row) => row.id)
        .sort();
    }

    // user-domains' wire contract: the sorted domains where the user holds at least
    // one grant — the accessible list itself, no filtering.
    return [...this.auth.domains()];
  });

  // current keeps the user's choice while the picker still offers it and snaps to
  // the first offered station when the list changes underneath it — a persona
  // switch, or turning showAll off while an inaccessible station is selected.
  readonly current = linkedSignal<string[], string>({
    source: this.waystations,
    computation: (stations, previous) =>
      previous !== undefined && stations.includes(previous.value) ? previous.value : (stations[0] ?? ''),
  });

  /** The client bound to the selected station; undefined while none is selected. */
  readonly station = computed<StationApi | undefined>(() => {
    const current = this.current();
    return current ? this.api.domain(current) : undefined;
  });

  constructor() {
    // Selecting a station re-scopes every permission question, so load that
    // station's digest into the client's cache. The load runs untracked — issued
    // inside the effect's reactive context it would adopt the interceptor's
    // loading-signal reads as dependencies and loop (see the class comment). A failed
    // load caches an empty digest: every question about the station answers false.
    effect(() => {
      const station = this.current();
      if (!station) return;
      untracked(() => void this.api.permissions.loadDigest(station as Domain).catch(() => undefined));
    });
  }

  /** The selected station's client, for handlers that run only while one is selected. */
  stationApi(): StationApi {
    const station = this.station();
    if (!station) {
      throw new Error('no waystation selected');
    }
    return station;
  }

  /**
   * can answers one permission question from the digest — the app's single gate for
   * requests and affordances. The client resolves the scope: a global resource asks
   * the global digest, a station-scoped resource or RPC method asks the selected
   * station's, and a station-scoped question answers false while no station is
   * selected. Conditional grants answer true — render, and let the server narrow
   * per row. Signal-backed, so lists and buttons re-evaluate when a digest loads or
   * the station changes.
   */
  can(permission: Permission, target: Resource | Method): boolean {
    this.permissions();
    return this.api.can(permission, target, (this.current() || undefined) as Domain | undefined);
  }

  /**
   * grantedFields is the digest's field-level enumeration for a resource — for Create,
   * the inputs a form is worth rendering (a denied field is absent; conditional
   * renders and the server judges the write). Undefined means the digest carries no
   * field-level entries for the permission — no field information — so narrow only on
   * a defined answer. Signal-backed like can().
   */
  grantedFields(permission: Permission, resource: Resource): readonly string[] | undefined {
    this.permissions();
    return this.api.grantedFields(permission, resource, (this.current() || undefined) as Domain | undefined);
  }

  setShowAll(all: boolean): void {
    this.showAll.set(all);
  }

  select(waystation: string): void {
    this.current.set(waystation);
  }

  /**
   * stationList derives a page's list from the selected station: `select` picks the
   * handle off the station-bound client, and the loader re-runs when the station
   * changes, sits idle (on the default empty list) while none is selected, and never
   * asks for a list the digest says the user cannot read. After a mutation, call
   * .reload() on the affected lists. Create resources in an injection context (a
   * component field initializer), so each one lives and dies with its page.
   */
  stationList<Row>(select: (station: StationApi) => ListHandle<Row>, query?: ListQuery<Row>): ResourceRef<Row[]> {
    return resource({
      params: () => {
        this.permissions();
        const station = this.station();
        const handle = station ? select(station) : undefined;
        return { handle: handle?.can(Permissions.List) ? handle : undefined };
      },
      loader: ({ params }) => (params.handle ? params.handle.list(query) : Promise.resolve([])),
      defaultValue: [],
    });
  }

  /** globalList is stationList's global-resource sibling: no station involved, same List gate. */
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
