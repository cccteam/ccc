import { DatePipe } from '@angular/common';
import { Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { MatSlideToggleModule } from '@angular/material/slide-toggle';
import { Methods, Permissions, Resources } from '@app/service/zz_gen_constants';
import { Workflows, RefitTasks, Refits, Ships } from '@app/service/zz_gen_resources';
import { Method, rowCapabilities } from '@cccteam/resource';
import { SectorService } from '../sector.service';
import { StarChartComponent } from '../star-chart/star-chart.component';
import { WorkflowGraphComponent } from '../workflow-graph/workflow-graph.component';

/**
 * The hangar deck: the refit workflow drawn as bays across the screen. Each ship in a
 * refit sits in the bay for its state; a bay is a drop target when the ship's own
 * Execute envelope lists the transition that leads there, so dropping the Good
 * Samaritan from In refit onto Flight test fires StartFlightTest, and three bays can
 * send a ship to the scrapyard (the multi-from edge, three chutes into one pit). Bays
 * you cannot drop into refuse the drop with the transition's name. Every affordance —
 * the estimate, the task ticks, the add-task form, the hail — is the row's own answer.
 */
@Component({
  selector: 'app-hangar-deck',
  imports: [
    DatePipe,
    FormsModule,
    MatButtonModule,
    MatCardModule,
    MatFormFieldModule,
    MatIconModule,
    MatInputModule,
    MatSelectModule,
    MatSlideToggleModule,
    StarChartComponent,
    WorkflowGraphComponent,
  ],
  templateUrl: './hangar-deck.component.html',
  styleUrl: './hangar-deck.component.scss',
})
export class HangarDeckComponent {
  private sectors = inject(SectorService);

  readonly methods = Methods;
  readonly resources = Resources;
  readonly workflow = Workflows.find((w) => w.root === Resources.Refits);
  // The bays in route order: the default first, then the states a transition leads
  // to, the scrapyard last.
  readonly bays = ['docked', 'inspected', 'in_refit', 'flight_test', 'cleared', 'scrapped'];

  refits = this.sectors.sectorList((sector) => sector.refits, { capabilities: ['Execute', 'Update', 'Create'] });
  tasks = this.sectors.sectorList((sector) => sector.refitTasks, { capabilities: ['Update'] });
  ships = this.sectors.sectorList((sector) => sector.ships, { capabilities: ['Execute'] });

  sector = this.sectors.current;
  canList = computed(() => this.sectors.can(Permissions.List, Resources.Refits));
  canOpen = computed(() => this.sectors.can(Permissions.Create, Resources.Refits));
  canListShips = computed(() => this.sectors.can(Permissions.List, Resources.Ships));

  selectedID = signal<string | undefined>(undefined);
  selected = computed(() => this.refits.value().find((r) => r.id === this.selectedID()));
  dragging = signal<string | undefined>(undefined);
  refusal = signal<string | undefined>(undefined);

  editEstimate: number | null = null;
  editNotes = '';
  newTaskInstructions = '';
  newRefitShipId = '';
  taskNotes: Record<string, string> = {};

  inBay(bay: string): Refits[] {
    return this.refits.value().filter((r) => r.statusId === bay);
  }

  shipName(id: string | undefined): string {
    return this.ships.value().find((s) => s.id === id)?.name ?? id ?? '—';
  }

  executable(refit: Refits): Method[] {
    return (rowCapabilities(refit)?.Execute ?? []) as Method[];
  }

  // The transition that moves a refit from its bay into the target bay, per the
  // generated Workflows constant — and whether this user's envelope lights it.
  edgeInto(refit: Refits, bay: string): { method: Method; live: boolean } | undefined {
    const t = this.workflow?.transitions.find((t) => t.to === bay && t.from.includes(refit.statusId ?? ''));
    if (!t) return undefined;
    return { method: t.method as Method, live: this.executable(refit).includes(t.method as Method) };
  }

  select(refit: Refits): void {
    this.selectedID.set(this.selectedID() === refit.id ? undefined : refit.id);
    this.editEstimate = refit.estimate ?? null;
    this.editNotes = refit.notes ?? '';
    this.newTaskInstructions = '';
    this.refusal.set(undefined);
  }

  onDragStart(refit: Refits): void {
    this.dragging.set(refit.id);
    this.refusal.set(undefined);
  }

  onDragOver(event: DragEvent): void {
    event.preventDefault();
  }

  async onDrop(bay: string): Promise<void> {
    const refit = this.refits.value().find((r) => r.id === this.dragging());
    this.dragging.set(undefined);
    if (!refit || refit.statusId === bay) return;
    const edge = this.edgeInto(refit, bay);
    if (!edge) {
      this.refusal.set(`No transition leads from ${refit.statusId} to ${bay}.`);
      return;
    }
    if (!edge.live) {
      this.refusal.set(`${edge.method} is not yours to fire on ${this.shipName(refit.shipId)}.`);
      return;
    }
    await this.fire(refit, edge.method);
  }

  async fire(refit: Refits, method: Method): Promise<void> {
    const api = this.sectors.sectorApi();
    const body = { refitId: refit.id };
    switch (method) {
      case Methods.InspectShip:
        await api.inspectShip.execute(body);
        break;
      case Methods.BeginRefit:
        await api.beginRefit.execute(body);
        break;
      case Methods.StartFlightTest:
        await api.startFlightTest.execute(body);
        break;
      case Methods.PassFlightTest:
        await api.passFlightTest.execute(body);
        break;
      case Methods.FailFlightTest:
        await api.failFlightTest.execute(body);
        break;
      case Methods.ScrapShip:
        await api.scrapShip.execute(body);
        break;
      default:
        return;
    }
    this.refits.reload();
    this.ships.reload();
  }

  canEdit(refit: Refits, field: keyof Refits & string): boolean {
    return this.sectors.sectorApi().refits.fieldEditable(refit, field);
  }

  canAddTask(refit: Refits): boolean {
    return this.sectors.sectorApi().refits.rowCan(refit, 'Create', Resources.RefitTasks);
  }

  canTick(task: RefitTasks): boolean {
    return this.sectors.sectorApi().refitTasks.fieldEditable(task, 'done');
  }

  canNote(task: RefitTasks): boolean {
    return this.sectors.sectorApi().refitTasks.fieldEditable(task, 'notes');
  }

  canHail(ship: Ships): boolean {
    return this.sectors.sectorApi().ships.rowCan(ship, 'Execute', Methods.HailShip);
  }

  tasksOf(refit: Refits): RefitTasks[] {
    return this.tasks
      .value()
      .filter((t) => t.refitId === refit.id)
      .sort((a, b) => a.taskNumber - b.taskNumber);
  }

  async saveEdits(refit: Refits): Promise<void> {
    const handle = this.sectors.sectorApi().refits;
    const patch: Record<string, unknown> = {};
    if (this.canEdit(refit, 'estimate') && this.editEstimate !== null && this.editEstimate !== refit.estimate) patch['estimate'] = this.editEstimate;
    if (this.canEdit(refit, 'notes') && this.editNotes !== (refit.notes ?? '')) patch['notes'] = this.editNotes;
    if (Object.keys(patch).length === 0) return;
    await handle.patch(handle.keyOf(refit), patch);
    this.refits.reload();
  }

  async addTask(refit: Refits): Promise<void> {
    if (!this.newTaskInstructions) return;
    const nextNumber = Math.max(0, ...this.tasksOf(refit).map((t) => t.taskNumber)) + 1;
    await this.sectors.sectorApi().refitTasks.create({
      refitId: refit.id,
      taskNumber: nextNumber,
      instructions: this.newTaskInstructions,
      done: false,
    });
    this.newTaskInstructions = '';
    this.tasks.reload();
  }

  async toggleTask(task: RefitTasks): Promise<void> {
    const handle = this.sectors.sectorApi().refitTasks;
    await handle.patch(handle.keyOf(task), { done: !task.done });
    this.tasks.reload();
  }

  async noteTask(task: RefitTasks): Promise<void> {
    const key = `${task.refitId}/${task.taskNumber}`;
    const notes = this.taskNotes[key];
    if (!notes) return;
    const handle = this.sectors.sectorApi().refitTasks;
    await handle.patch(handle.keyOf(task), { notes });
    this.tasks.reload();
  }

  taskKey(task: RefitTasks): string {
    return `${task.refitId}/${task.taskNumber}`;
  }

  async hail(ship: Ships): Promise<void> {
    await this.sectors.sectorApi().hailShip.execute({ shipId: ship.id });
    this.ships.reload();
  }

  async openRefit(): Promise<void> {
    if (!this.newRefitShipId) return;
    await this.sectors.sectorApi().refits.create({ shipId: this.newRefitShipId });
    this.newRefitShipId = '';
    this.refits.reload();
  }
}
