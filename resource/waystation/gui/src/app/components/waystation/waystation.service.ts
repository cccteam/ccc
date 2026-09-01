import { HttpClient, httpResource, HttpResourceRef } from '@angular/common/http';
import { inject, Injectable, signal } from '@angular/core';
import {
  IncidentReports,
  Requisitions,
  RequisitionLines,
  WorkOrders,
  WorkOrderTasks,
} from '@app/service/zz_gen_resources';
import { API_URL } from '@cccteam/ccc-lib/types';
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

  readonly waystations = signal<string[]>([]);
  readonly current = signal('');

  loadDirectory(): void {
    this.http.get<{ waystations: string[] }>(`${this.apiUrl}/waystation-directory`).subscribe((res) => {
      this.waystations.set(res.waystations ?? []);
      const first = this.waystations()[0];
      if (!this.current() && first) {
        this.current.set(first);
      }
    });
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
