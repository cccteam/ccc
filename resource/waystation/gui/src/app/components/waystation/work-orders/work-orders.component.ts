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
import { Methods, Permissions, Resources } from '@app/service/zz_gen_constants';
import { WorkOrderStatus } from '@app/service/zz_gen_enums';
import { Assets, Teams, WorkOrders, WorkOrderTasks } from '@app/service/zz_gen_resources';
import { WaystationService } from '../waystation.service';
import { WaystationSelectComponent } from '../waystation-select/waystation-select.component';

/**
 * The work-order board is the stateful-resource showcase: the status column is
 * structurally unwritable from the wire, every transition runs through an
 * Execute-gated RPC, and per-state conditional grants decide who sees and does what
 * (technicians see their teams' orders, auditors see terminal ones, a station chief
 * may delete drafts only).
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

  // Sorted by last activity server-side: updatedAt is the mechanical enforcement
  // stamp, bumped by every update — including a Nudge, which changes nothing else.
  // Untouched rows (updatedAt unset) sort last.
  orders = this.ws.stationList<WorkOrders>('work-orders?sort=updatedAt:desc', Resources.WorkOrders);
  taskRows = this.ws.stationList<WorkOrderTasks>('work-order-tasks', Resources.WorkOrderTasks);
  teams = this.ws.stationList<Teams>('teams', Resources.Teams);
  assets = this.ws.stationList<Assets>('assets', Resources.Assets);
  columns = ['title', 'status', 'priority', 'team', 'dueAt', 'lastActivity', 'actions'];

  // Affordances from the selected station's digest: an absent grant hides the
  // surface, a conditional one renders it and lets the server narrow per row.
  station = this.ws.current;
  canList = computed(() => this.ws.can(Permissions.List, Resources.WorkOrders));
  canCreate = computed(() => this.ws.can(Permissions.Create, Resources.WorkOrders));
  canDelete = computed(() => this.ws.can(Permissions.Delete, Resources.WorkOrders));
  canSchedule = computed(() => this.ws.can(Permissions.Execute, Methods.ScheduleWorkOrder));
  canStart = computed(() => this.ws.can(Permissions.Execute, Methods.StartWorkOrder));
  canComplete = computed(() => this.ws.can(Permissions.Execute, Methods.CompleteWorkOrder));
  canNudge = computed(() => this.ws.can(Permissions.Execute, Methods.NudgeWorkOrder));
  canAddTask = computed(() => this.ws.can(Permissions.Create, Resources.WorkOrderTasks));
  canToggleTask = computed(() => this.ws.can(Permissions.Update, Resources.WorkOrderTasks));

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

  create(): void {
    if (!this.newTitle || this.newPriority === null || !this.newAssetId) {
      return;
    }
    this.ws
      .createWorkOrder({
        title: this.newTitle,
        summary: this.newSummary,
        priority: this.newPriority,
        assetId: this.newAssetId,
      })
      .subscribe(() => {
        this.newTitle = '';
        this.newSummary = '';
        this.newPriority = null;
        this.newAssetId = '';
        this.orders.reload();
      });
  }

  schedule(order: WorkOrders): void {
    if (!this.scheduleTeamId || !this.scheduleDueAt) {
      return;
    }
    this.ws
      .scheduleWorkOrder(order.id, this.scheduleTeamId, new Date(this.scheduleDueAt).toISOString())
      .subscribe(() => this.orders.reload());
  }

  start(order: WorkOrders): void {
    this.ws.startWorkOrder(order.id).subscribe(() => this.orders.reload());
  }

  complete(order: WorkOrders): void {
    this.ws.completeWorkOrder(order.id).subscribe(() => this.orders.reload());
  }

  // Nudge flags a stalled order for attention without changing it: the touch bumps
  // updatedAt (so the order jumps to the top of the last-activity sort) and the
  // audit trail records who nudged. Chiefs and foremen hold the grant.
  nudge(order: WorkOrders): void {
    this.ws.nudgeWorkOrder(order.id).subscribe(() => this.orders.reload());
  }

  terminal(order: WorkOrders): boolean {
    return order.statusId === this.status.Completed || order.statusId === this.status.Cancelled;
  }

  remove(order: WorkOrders): void {
    this.ws.deleteWorkOrder(order.id).subscribe(() => {
      this.selectedID.set(undefined);
      this.orders.reload();
    });
  }

  addTask(order: WorkOrders): void {
    if (!this.newTaskInstructions) {
      return;
    }
    const nextNumber = Math.max(0, ...this.tasks(order).map((task) => task.taskNumber)) + 1;
    this.ws
      .createWorkOrderTask(order.id, nextNumber, { instructions: this.newTaskInstructions, done: false })
      .subscribe(() => {
        this.newTaskInstructions = '';
        this.taskRows.reload();
      });
  }

  toggleTask(task: WorkOrderTasks): void {
    this.ws.setTaskDone(task.workOrderId, task.taskNumber, !task.done).subscribe(() => this.taskRows.reload());
  }
}
