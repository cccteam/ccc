import { Component, computed, inject, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatChipsModule } from '@angular/material/chips';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { MatSlideToggleModule } from '@angular/material/slide-toggle';
import { MatTableModule } from '@angular/material/table';
import { Method } from '@cccteam/resource';
import { Methods, Permissions, Resources } from '@app/service/zz_gen_constants';
import { WorkOrderStatus } from '@app/service/zz_gen_enums';
import { WorkOrders, WorkOrderTasks } from '@app/service/zz_gen_resources';
import { WaystationService } from '../waystation.service';
import { WaystationSelectComponent } from '../waystation-select/waystation-select.component';

/**
 * The work-order board is the stateful-resource showcase: the status column is
 * structurally unwritable from the wire (it is absent from WorkOrdersCreate and
 * WorkOrdersPatch), every transition runs through an Execute-gated RPC handle, and
 * per-state conditional grants decide who sees and does what (technicians see their
 * teams' orders, auditors see terminal ones, a station chief may delete drafts only).
 */
@Component({
  selector: 'app-work-orders',
  imports: [
    DatePipe,
    FormsModule,
    MatButtonModule,
    MatCardModule,
    MatChipsModule,
    MatFormFieldModule,
    MatIconModule,
    MatInputModule,
    MatSelectModule,
    MatSlideToggleModule,
    MatTableModule,
    WaystationSelectComponent,
  ],
  templateUrl: './work-orders.component.html',
  styleUrl: './work-orders.component.scss',
})
export class WorkOrdersComponent {
  private ws = inject(WaystationService);

  readonly status = WorkOrderStatus;

  readonly methods = Methods;

  // Sorted by last activity server-side: updatedAt is the mechanical enforcement
  // stamp, bumped by every update — including a Nudge, which changes nothing else.
  // Untouched rows (updatedAt unset) sort last. The sort field is typed against the
  // row, so a misspelling does not compile. The capability envelope rides every row:
  // Execute answers which declared transitions apply to its state, Delete whether
  // the delete is live — the same answers the server enforces.
  orders = this.ws.stationList((station) => station.workOrders, {
    sort: { field: 'updatedAt', direction: 'desc' },
    capabilities: ['Execute', 'Delete'],
  });
  taskRows = this.ws.stationList((station) => station.workOrderTasks);
  teams = this.ws.stationList((station) => station.teams);
  assets = this.ws.stationList((station) => station.assets);
  columns = ['title', 'status', 'priority', 'team', 'dueAt', 'lastActivity', 'actions'];

  // Affordances from the selected station's digest: an absent grant hides the
  // surface, a conditional one renders it and lets the server narrow per row.
  station = this.ws.current;
  canList = computed(() => this.ws.can(Permissions.List, Resources.WorkOrders));
  canCreate = computed(() => this.ws.can(Permissions.Create, Resources.WorkOrders));
  canNudge = computed(() => this.ws.can(Permissions.Execute, Methods.NudgeWorkOrder));
  canAddTask = computed(() => this.ws.can(Permissions.Create, Resources.WorkOrderTasks));
  canToggleTask = computed(() => this.ws.can(Permissions.Update, Resources.WorkOrderTasks));

  // Per-row affordances from the capability envelope: no statusId comparisons here —
  // whether a transition button or the delete renders is the row's own answer.
  canRun(order: WorkOrders, method: Method): boolean {
    return this.ws.stationApi().workOrders.rowCan(order, 'Execute', method);
  }

  canRemove(order: WorkOrders): boolean {
    return this.ws.stationApi().workOrders.rowCan(order, 'Delete');
  }

  tasksByOrder = computed(() => {
    const grouped = new Map<string, WorkOrderTasks[]>();
    for (const task of this.taskRows.value()) {
      const list = grouped.get(task.workOrderId) ?? [];
      list.push(task);
      grouped.set(task.workOrderId, list);
    }
    for (const list of grouped.values()) {
      list.sort((a, b) => a.taskNumber - b.taskNumber);
    }
    return grouped;
  });

  // The selection resolves against the live list: a station switch or reload that
  // drops the row clears it naturally.
  selectedID = signal<string | undefined>(undefined);
  selected = computed(() => this.orders.value().find((order) => order.id === this.selectedID()));

  // Schedule form state, shown when scheduling the selected draft.
  scheduleTeamId = '';
  scheduleDueAt = '';

  // Create form state.
  newTitle = '';
  newSummary = '';
  newPriority: number | null = null;
  newAssetId = '';

  // New task form state for the selected work order.
  newTaskInstructions = '';

  select(order: WorkOrders): void {
    this.selectedID.set(this.selectedID() === order.id ? undefined : order.id);
    this.scheduleTeamId = '';
    this.scheduleDueAt = '';
    this.newTaskInstructions = '';
  }

  teamName(teamId: string | undefined): string {
    return this.teams.value().find((team) => team.id === teamId)?.name ?? '';
  }

  tasks(order: WorkOrders): WorkOrderTasks[] {
    return this.tasksByOrder().get(order.id) ?? [];
  }

  // WorkOrdersCreate is the wire's create shape: the tenant key and the status are
  // server-owned and absent, so neither can be sent by mistake.
  async create(): Promise<void> {
    if (!this.newTitle || this.newPriority === null || !this.newAssetId) {
      return;
    }
    await this.ws.stationApi().workOrders.create({
      title: this.newTitle,
      summary: this.newSummary,
      priority: this.newPriority,
      assetId: this.newAssetId,
    });
    this.newTitle = '';
    this.newSummary = '';
    this.newPriority = null;
    this.newAssetId = '';
    this.orders.reload();
  }

  async schedule(order: WorkOrders): Promise<void> {
    if (!this.scheduleTeamId || !this.scheduleDueAt) {
      return;
    }
    await this.ws.stationApi().scheduleWorkOrder.execute({
      workOrderId: order.id,
      assignedTeamId: this.scheduleTeamId,
      dueAt: new Date(this.scheduleDueAt),
    });
    this.orders.reload();
  }

  async start(order: WorkOrders): Promise<void> {
    await this.ws.stationApi().startWorkOrder.execute({ workOrderId: order.id });
    this.orders.reload();
  }

  async complete(order: WorkOrders): Promise<void> {
    await this.ws.stationApi().completeWorkOrder.execute({ workOrderId: order.id });
    this.orders.reload();
  }

  // Nudge flags a stalled order for attention without changing it: the touch bumps
  // updatedAt (so the order jumps to the top of the last-activity sort) and the
  // audit trail records who nudged. Chiefs and foremen hold the grant.
  async nudge(order: WorkOrders): Promise<void> {
    await this.ws.stationApi().nudgeWorkOrder.execute({ workOrderId: order.id });
    this.orders.reload();
  }

  terminal(order: WorkOrders): boolean {
    return order.statusId === this.status.Completed || order.statusId === this.status.Cancelled;
  }

  async remove(order: WorkOrders): Promise<void> {
    const workOrders = this.ws.stationApi().workOrders;
    await workOrders.remove(workOrders.keyOf(order));
    this.selectedID.set(undefined);
    this.orders.reload();
  }

  // Tasks carry a compound, client-assigned key: WorkOrderTasksCreate requires both
  // parts, and the client lifts them out of the value into the operation path.
  async addTask(order: WorkOrders): Promise<void> {
    if (!this.newTaskInstructions) {
      return;
    }
    const nextNumber = Math.max(0, ...this.tasks(order).map((task) => task.taskNumber)) + 1;
    await this.ws.stationApi().workOrderTasks.create({
      workOrderId: order.id,
      taskNumber: nextNumber,
      instructions: this.newTaskInstructions,
      done: false,
    });
    this.newTaskInstructions = '';
    this.taskRows.reload();
  }

  async toggleTask(task: WorkOrderTasks): Promise<void> {
    const tasks = this.ws.stationApi().workOrderTasks;
    await tasks.patch(tasks.keyOf(task), { done: !task.done });
    this.taskRows.reload();
  }
}
