import { HttpClient } from '@angular/common/http';
import { inject, Injectable, signal } from '@angular/core';
import {
  Assets,
  CatalogItems,
  IncidentReports,
  InventoryLots,
  Requisitions,
  RequisitionLines,
  Shipments,
  StationStatusBoards,
  Teams,
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

  private list<T>(resourceRoute: string): Observable<T[]> {
    return this.http.get<T[]>(`${this.apiUrl}/waystations/${this.current()}/${resourceRoute}`);
  }

  // Sorted by last activity server-side: updatedAt is the mechanical enforcement
  // stamp, bumped by every update — including a Nudge, which changes nothing else.
  // Untouched rows (updatedAt unset) sort last.
  workOrders(): Observable<WorkOrders[]> {
    return this.list<WorkOrders>('work-orders?sort=updatedAt:desc');
  }

  workOrderTasks(): Observable<WorkOrderTasks[]> {
    return this.list<WorkOrderTasks>('work-order-tasks');
  }

  teams(): Observable<Teams[]> {
    return this.list<Teams>('teams');
  }

  assets(): Observable<Assets[]> {
    return this.list<Assets>('assets');
  }

  requisitions(): Observable<Requisitions[]> {
    return this.list<Requisitions>('requisitions');
  }

  requisitionLines(): Observable<RequisitionLines[]> {
    return this.list<RequisitionLines>('requisition-lines');
  }

  shipments(): Observable<Shipments[]> {
    return this.list<Shipments>('shipments');
  }

  inventoryLots(): Observable<InventoryLots[]> {
    // Sorting is done server-side through the reserved sort parameter: soonest
    // expiry first (Spanner sorts NULL expiries — never expiring — to the top).
    return this.list<InventoryLots>('inventory-lots?sort=expiresOn');
  }

  incidents(): Observable<IncidentReports[]> {
    return this.list<IncidentReports>('incident-reports');
  }

  statusBoard(): Observable<StationStatusBoards[]> {
    return this.list<StationStatusBoards>('station-status-boards');
  }

  catalogItems(): Observable<CatalogItems[]> {
    return this.http.get<CatalogItems[]>(`${this.apiUrl}/catalog-items`);
  }

  auditTrail(): Observable<AuditTrailEntry[]> {
    return this.http.get<AuditTrailEntry[]>(`${this.apiUrl}/audit-trail-entries`);
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
