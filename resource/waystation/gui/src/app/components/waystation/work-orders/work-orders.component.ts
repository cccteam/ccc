import { Component, inject, signal } from '@angular/core';
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
import { WorkOrderStatus } from '@app/service/zz_gen_enums';
import { Assets, Teams, WorkOrders, WorkOrderTasks } from '@app/service/zz_gen_resources';
import { reloadOnStationChange, WaystationService } from '../waystation.service';
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

  orders = signal<WorkOrders[]>([]);
  tasksByOrder = signal<Map<string, WorkOrderTasks[]>>(new Map());
  teams = signal<Teams[]>([]);
  assets = signal<Assets[]>([]);
  columns = ['title', 'status', 'priority', 'team', 'dueAt', 'lastActivity', 'actions'];

  selected = signal<WorkOrders | undefined>(undefined);

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

  constructor() {
    reloadOnStationChange(this.ws, () => {
      this.selected.set(undefined);
      this.load();
    });
  }

  load(): void {
    this.ws.workOrders().subscribe({
      next: (orders) => {
        this.orders.set(orders ?? []);
        const current = this.selected();
        if (current) {
          this.selected.set(this.orders().find((order) => order.id === current.id));
        }
      },
      error: () => this.orders.set([]),
    });
    this.ws.workOrderTasks().subscribe({
      next: (tasks) => {
        const grouped = new Map<string, WorkOrderTasks[]>();
        for (const task of tasks ?? []) {
          const list = grouped.get(task.workOrderId) ?? [];
          list.push(task);
          grouped.set(task.workOrderId, list);
        }
        for (const list of grouped.values()) {
          list.sort((a, b) => a.taskNumber - b.taskNumber);
        }
        this.tasksByOrder.set(grouped);
      },
      error: () => this.tasksByOrder.set(new Map()),
    });
    this.ws.teams().subscribe({
      next: (teams) => this.teams.set(teams ?? []),
      error: () => this.teams.set([]),
    });
    this.ws.assets().subscribe({
      next: (assets) => this.assets.set(assets ?? []),
      error: () => this.assets.set([]),
    });
  }

  select(order: WorkOrders): void {
    this.selected.set(this.selected()?.id === order.id ? undefined : order);
    this.scheduleTeamId = '';
    this.scheduleDueAt = '';
    this.newTaskInstructions = '';
  }

  teamName(teamId: string | undefined): string {
    return this.teams().find((team) => team.id === teamId)?.name ?? '';
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
        this.load();
      });
  }

  schedule(order: WorkOrders): void {
    if (!this.scheduleTeamId || !this.scheduleDueAt) {
      return;
    }
    this.ws
      .scheduleWorkOrder(order.id, this.scheduleTeamId, new Date(this.scheduleDueAt).toISOString())
      .subscribe(() => this.load());
  }

  start(order: WorkOrders): void {
    this.ws.startWorkOrder(order.id).subscribe(() => this.load());
  }

  complete(order: WorkOrders): void {
    this.ws.completeWorkOrder(order.id).subscribe(() => this.load());
  }

  // Nudge flags a stalled order for attention without changing it: the touch bumps
  // updatedAt (so the order jumps to the top of the last-activity sort) and the
  // audit trail records who nudged. Chiefs and foremen hold the grant.
  nudge(order: WorkOrders): void {
    this.ws.nudgeWorkOrder(order.id).subscribe(() => this.load());
  }

  terminal(order: WorkOrders): boolean {
    return order.statusId === this.status.Completed || order.statusId === this.status.Cancelled;
  }

  remove(order: WorkOrders): void {
    this.ws.deleteWorkOrder(order.id).subscribe(() => {
      this.selected.set(undefined);
      this.load();
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
        this.load();
      });
  }

  toggleTask(task: WorkOrderTasks): void {
    this.ws.setTaskDone(task.workOrderId, task.taskNumber, !task.done).subscribe(() => this.load());
  }
}
