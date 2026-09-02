import { HttpClient, httpResource, HttpResourceRef } from '@angular/common/http';
import { computed, effect, inject, Injectable, linkedSignal, signal, untracked } from '@angular/core';
import {
  IncidentReports,
  Requisitions,
  RequisitionLines,
  Waystations,
  WorkOrders,
  WorkOrderTasks,
} from '@app/service/zz_gen_resources';
import { AuthService } from '@cccteam/ccc-lib/auth-service';
import { API_URL, Domain } from '@cccteam/ccc-lib/types';
import { Observable } from 'rxjs';

interface Operation {
  op: 'add' | 'patch' | 'remove';
  path: string;
  value?: unknown;
}

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
 * WaystationService talks to the waystation-scoped API surface by hand: the
 * config-driven resource components cannot fill the {waystationID} segment of a
 * domain route, so these pages address /api/waystations/{waystationID}/... directly.
 * Mutations go through the consolidated /api/resources endpoint — the same
 * JSON-Patch-style surface the integration suites pin — and workflow transitions go
 * through the Execute-gated RPC routes.
 *
 * The selected waystation is shared state: it is the permission domain for every
 * request these pages make, so switching it re-scopes what each persona can see.
 *
 * Data loading is DECLARATIVE: each page's lists are httpResources derived from the
 * current-waystation signal (stationList/globalList below), never HTTP issued from
 * an effect. An effect that fetches adopts hidden dependencies — ccc-lib's
 * ApiInterceptor reads the global loading signal synchronously on every request
 * (UiCoreService.beginActivity), so a fetching effect re-runs on every response's
 * endActivity write: an infinite request loop, as fast as the server can answer.
 * httpResource loaders run untracked by design, which rules that loop out
 * structurally.
 */
@Injectable({ providedIn: 'root' })
export class WaystationService {
  private http = inject(HttpClient);
  private apiUrl = inject(API_URL);

  private auth = inject(AuthService);

  // showAll widens the picker from the permission-derived list to the full tenant
  // roster. A real application would not offer it; the demo keeps it as the
  // clickable path to fail-closed refusals — select a station you hold no roles at
  // and every list load on the page refuses.
  readonly showAll = signal(false);

  // The picker has no bespoke endpoint: its two questions are answered by surfaces
  // the library and the application already serve. "Where do I hold domain-scoped
  // grants" is the generated user-domains endpoint, loaded once per session and
  // cached on AuthService as domains(). "What does the fleet roster look like" is the
  // generated, permission-checked Waystations resource, fetched only while showAll is
  // on; it idles until the session authenticates and resets at logout.
  private roster = httpResource<Waystations[]>(
    () => (this.auth.authenticated() && this.showAll() ? `${this.apiUrl}/waystations` : undefined),
    { defaultValue: [] },
  );

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

  constructor() {
    // Selecting a station re-scopes every permission question, so load that
    // station's digest: ccc-lib's hasPermission then answers for its resources. The
    // fetch runs untracked — issued inside the effect's reactive context it would
    // adopt the interceptor's loading-signal reads as dependencies and loop (see the
    // class comment).
    effect(() => {
      const station = this.current();
      if (!station) return;
      untracked(() => this.auth.loadDigest(station as Domain).subscribe());
    });
  }

  setShowAll(all: boolean): void {
    this.showAll.set(all);
  }

  select(waystation: string): void {
    this.current.set(waystation);
  }

  /**
   * stationList derives a page's list from the selected waystation: the request URL
   * is a function of current(), so the resource refetches when the station changes
   * and sits idle (on the default empty list) while none is selected. After a
   * mutation, call .reload() on the affected lists. Create resources in an
   * injection context (a component field initializer), so each one lives and dies
   * with its page.
   */
  stationList<T>(resourceRoute: string): HttpResourceRef<T[]> {
    return httpResource<T[]>(
      () => (this.current() ? `${this.apiUrl}/waystations/${this.current()}/${resourceRoute}` : undefined),
      { defaultValue: [] },
    );
  }

  /** globalList is stationList's global-resource sibling: one fixed URL, no domain segment. */
  globalList<T>(resourceRoute: string): HttpResourceRef<T[]> {
    return httpResource<T[]>(() => `${this.apiUrl}/${resourceRoute}`, { defaultValue: [] });
  }

  // ops sends consolidated mutations; paths are rooted at the API, e.g.
  // /waystations/ws-alpha/work-orders/{id}.
  private ops(operations: Operation[]): Observable<Record<string, string[]>> {
    return this.http.patch<Record<string, string[]>>(`${this.apiUrl}/resources`, operations);
  }

  private domainPath(resourceRoute: string, keys: (string | number)[] = []): string {
    return ['', 'waystations', this.current(), resourceRoute, ...keys.map(String)].join('/');
  }

  createWorkOrder(value: Partial<WorkOrders>): Observable<Record<string, string[]>> {
    return this.ops([
      { op: 'add', path: this.domainPath('work-orders'), value: { ...value, waystationId: this.current() } },
    ]);
  }

  createWorkOrderTask(workOrderId: string, taskNumber: number, value: Partial<WorkOrderTasks>): Observable<unknown> {
    return this.ops([{ op: 'add', path: this.domainPath('work-order-tasks', [workOrderId, taskNumber]), value }]);
  }

  setTaskDone(workOrderId: string, taskNumber: number, done: boolean): Observable<unknown> {
    return this.ops([
      { op: 'patch', path: this.domainPath('work-order-tasks', [workOrderId, taskNumber]), value: { done } },
    ]);
  }

  deleteWorkOrder(id: string): Observable<unknown> {
    return this.ops([{ op: 'remove', path: this.domainPath('work-orders', [id]) }]);
  }

  createRequisition(value: Partial<Requisitions>): Observable<Record<string, string[]>> {
    return this.ops([
      { op: 'add', path: this.domainPath('requisitions'), value: { ...value, waystationId: this.current() } },
    ]);
  }

  createRequisitionLine(
    requisitionId: string,
    lineNumber: number,
    value: Partial<RequisitionLines>,
  ): Observable<unknown> {
    return this.ops([{ op: 'add', path: this.domainPath('requisition-lines', [requisitionId, lineNumber]), value }]);
  }

  removeRequisitionLine(requisitionId: string, lineNumber: number): Observable<unknown> {
    return this.ops([{ op: 'remove', path: this.domainPath('requisition-lines', [requisitionId, lineNumber]) }]);
  }

  createIncident(value: Partial<IncidentReports>): Observable<unknown> {
    return this.ops([
      { op: 'add', path: this.domainPath('incident-reports'), value: { ...value, waystationId: this.current() } },
    ]);
  }

  deleteInventoryLot(id: string): Observable<unknown> {
    return this.ops([{ op: 'remove', path: this.domainPath('inventory-lots', [id]) }]);
  }

  private rpc(methodRoute: string, body: unknown): Observable<unknown> {
    return this.http.post(`${this.apiUrl}/waystations/${this.current()}/${methodRoute}`, body);
  }

  scheduleWorkOrder(workOrderId: string, assignedTeamId: string, dueAt: string): Observable<unknown> {
    return this.rpc('schedule-work-order', { workOrderId, assignedTeamId, dueAt });
  }

  startWorkOrder(workOrderId: string): Observable<unknown> {
    return this.rpc('start-work-order', { workOrderId });
  }

  completeWorkOrder(workOrderId: string): Observable<unknown> {
    return this.rpc('complete-work-order', { workOrderId });
  }

  // Nudge is the first-class Touch: the update pipeline runs with no caller-set
  // fields, so the order's updatedAt bumps and the audit trail records who nudged
  // while nothing about the order changes.
  nudgeWorkOrder(workOrderId: string): Observable<unknown> {
    return this.rpc('nudge-work-order', { workOrderId });
  }

  submitRequisition(requisitionId: string): Observable<unknown> {
    return this.rpc('submit-requisition', { requisitionId });
  }

  approveRequisition(requisitionId: string): Observable<unknown> {
    return this.rpc('approve-requisition', { requisitionId });
  }

  declineRequisition(requisitionId: string, reason: string): Observable<unknown> {
    return this.rpc('decline-requisition', { requisitionId, reason });
  }

  receiveShipment(shipmentId: string): Observable<unknown> {
    return this.rpc('receive-shipment', { shipmentId });
  }
}
