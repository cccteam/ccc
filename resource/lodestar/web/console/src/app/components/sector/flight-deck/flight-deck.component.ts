import { DatePipe, DecimalPipe } from '@angular/common';
import { Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { MatTableModule } from '@angular/material/table';
import { Methods, Permissions, Resources } from '@app/service/zz_gen_constants';
import { FailReason, MissionKind } from '@app/service/zz_gen_enums';
import { Missions, Sorties, SortieExpenses } from '@app/service/zz_gen_resources';
import { Method, rowCapabilities } from '@cccteam/resource';
import { SectorService } from '../sector.service';
import { StarChartComponent } from '../star-chart/star-chart.component';
import { WorkflowGraphComponent } from '../workflow-graph/workflow-graph.component';

/**
 * The flight deck: every mission is a live state graph. Call sheets list what the
 * digest lets this persona see (a masked fee prints a REDACTED stamp, an overdue
 * deadline turns red); opening one draws the mission workflow from the generated
 * Workflows constant with the current state lit and the edges the row's
 * zzCapabilities.Execute list names drawn live. Beneath it, Add sortie renders when the
 * mission row's Create list names Sorties, and a sortie's Add expense when the sortie
 * row's list names SortieExpenses. No page copies a state rule.
 */
@Component({
  selector: 'app-flight-deck',
  imports: [
    DatePipe,
    DecimalPipe,
    FormsModule,
    MatButtonModule,
    MatCardModule,
    MatFormFieldModule,
    MatIconModule,
    MatInputModule,
    MatSelectModule,
    MatTableModule,
    StarChartComponent,
    WorkflowGraphComponent,
  ],
  templateUrl: './flight-deck.component.html',
  styleUrl: './flight-deck.component.scss',
})
export class FlightDeckComponent {
  private sectors = inject(SectorService);

  readonly methods = Methods;
  readonly resources = Resources;
  readonly kinds = Object.values(MissionKind);
  readonly failReasons = Object.values(FailReason);
  readonly now = signal(new Date());

  missions = this.sectors.sectorList((sector) => sector.missions, {
    sort: { field: 'deadline' },
    capabilities: ['Execute', 'Create', 'Update', 'Delete'],
  });
  sorties = this.sectors.sectorList((sector) => sector.sorties, { capabilities: ['Create', 'Update'] });
  expenses = this.sectors.sectorList((sector) => sector.sortieExpenses, { capabilities: ['Update', 'Delete'] });
  squadrons = this.sectors.sectorList((sector) => sector.squadrons);
  ships = this.sectors.sectorList((sector) => sector.ships);
  clients = this.sectors.globalList((api) => api.clients);
  pilots = this.sectors.globalList((api) => api.pilots);

  sector = this.sectors.current;
  canList = computed(() => this.sectors.can(Permissions.List, Resources.Missions));
  canBook = computed(() => this.sectors.can(Permissions.Create, Resources.Missions));
  bookableFields = computed(() => this.sectors.grantedFields(Permissions.Create, Resources.Missions));

  selectedID = signal<string | undefined>(undefined);
  selected = computed(() => this.missions.value().find((m) => m.id === this.selectedID()));
  pendingEdge = signal<Method | undefined>(undefined);

  // Transition bodies that need input beyond the target row.
  claimSquadronId = '';
  launchShipId = '';
  launchPilotUserId = '';
  holdReason = '';
  failReasonId = '';

  // Edit form state (the Update envelope decides which fields render).
  editNotes = '';
  editDeadline = '';
  editAssignedSquadronId = '';
  editFee: number | null = null;

  // Booking form state.
  newTitle = '';
  newBrief = '';
  newKind = '';
  newClientId = '';
  newHazard: number | null = null;
  newFee: number | null = null;
  newDeadline = '';
  newNotes = '';

  // Add-sortie and add-expense form state.
  newSortieShipId = '';
  newSortiePilot = '';
  newExpenseCategory = 'fuel';
  newExpenseAmount: number | null = null;

  constructor() {
    setInterval(() => this.now.set(new Date()), 1000);
  }

  select(mission: Missions): void {
    this.selectedID.set(this.selectedID() === mission.id ? undefined : mission.id);
    this.pendingEdge.set(undefined);
    this.editNotes = mission.notes ?? '';
    this.editDeadline = '';
    this.editAssignedSquadronId = mission.assignedSquadronId ?? '';
    this.editFee = null;
    this.claimSquadronId = '';
    this.launchShipId = '';
    this.launchPilotUserId = '';
    this.holdReason = '';
    this.failReasonId = '';
  }

  executable(mission: Missions): Method[] {
    return (rowCapabilities(mission)?.Execute ?? []) as Method[];
  }

  canEdit(mission: Missions, field: keyof Missions & string): boolean {
    return this.sectors.sectorApi().missions.fieldEditable(mission, field);
  }

  canRemove(mission: Missions): boolean {
    return this.sectors.sectorApi().missions.rowCan(mission, 'Delete');
  }

  canAddSortie(mission: Missions): boolean {
    return this.sectors.sectorApi().missions.rowCan(mission, 'Create', Resources.Sorties);
  }

  canAddExpense(sortie: Sorties): boolean {
    return this.sectors.sectorApi().sorties.rowCan(sortie, 'Create', Resources.SortieExpenses);
  }

  canBookField(field: string): boolean {
    const fields = this.bookableFields();
    return fields === undefined || fields.includes(field);
  }

  sortiesOf(mission: Missions): Sorties[] {
    return this.sorties.value().filter((s) => s.missionId === mission.id);
  }

  expensesOf(sortie: Sorties): SortieExpenses[] {
    return this.expenses.value().filter((e) => e.sortieId === sortie.id);
  }

  squadronName(id: string | null | undefined): string {
    return this.squadrons.value().find((s) => s.id === id)?.name ?? '—';
  }

  clientName(id: string | undefined): string {
    return this.clients.value().find((c) => c.id === id)?.name ?? '—';
  }

  // A masked cell arrives as an ABSENT key: the sheet prints a stamp, never a zero.
  feeMasked(mission: Missions): boolean {
    return !('fee' in mission);
  }

  overdue(mission: Missions): boolean {
    return !!mission.deadline && new Date(mission.deadline).getTime() < this.now().getTime();
  }

  countdown(mission: Missions): string {
    if (!mission.deadline) return '—';
    const ms = new Date(mission.deadline).getTime() - this.now().getTime();
    const sign = ms < 0 ? '-' : '';
    const abs = Math.abs(ms) / 1000;
    const d = Math.floor(abs / 86400);
    const h = Math.floor((abs % 86400) / 3600);
    const m = Math.floor((abs % 3600) / 60);
    const s = Math.floor(abs % 60);
    return d > 0 ? `${sign}${d}d ${h}h` : `${sign}${h}h ${m}m ${s}s`;
  }

  pips(hazard: number | undefined): string {
    const n = hazard ?? 0;
    return '●'.repeat(n) + '○'.repeat(Math.max(0, 5 - n));
  }

  // Which extra input a transition needs; the graph's click lands here.
  private needsInput(method: Method): boolean {
    return (
      method === Methods.ClaimMission ||
      method === Methods.LaunchMission ||
      method === Methods.HoldMission ||
      method === Methods.FailMission
    );
  }

  onEdge(mission: Missions, method: Method): void {
    if (this.needsInput(method)) {
      this.pendingEdge.set(method);
      return;
    }
    void this.fire(mission, method);
  }

  async fire(mission: Missions, method: Method): Promise<void> {
    const api = this.sectors.sectorApi();
    switch (method) {
      case Methods.ClaimMission:
        if (!this.claimSquadronId) return;
        await api.claimMission.execute({ missionId: mission.id, squadronId: this.claimSquadronId });
        break;
      case Methods.LaunchMission:
        if (!this.launchShipId || !this.launchPilotUserId) return;
        await api.launchMission.execute({
          missionId: mission.id,
          shipId: this.launchShipId,
          pilotUserId: this.launchPilotUserId,
        });
        break;
      case Methods.HoldMission:
        if (!this.holdReason) return;
        await api.holdMission.execute({ missionId: mission.id, reason: this.holdReason });
        break;
      case Methods.ResumeMission:
        await api.resumeMission.execute({ missionId: mission.id });
        break;
      case Methods.StandDownMission:
        await api.standDownMission.execute({ missionId: mission.id });
        break;
      case Methods.CompleteMission:
        await api.completeMission.execute({ missionId: mission.id });
        break;
      case Methods.FailMission:
        if (!this.failReasonId) return;
        await api.failMission.execute({ missionId: mission.id, reasonId: this.failReasonId });
        break;
      default:
        return;
    }
    this.pendingEdge.set(undefined);
    this.missions.reload();
    this.sorties.reload();
  }

  async saveEdits(mission: Missions): Promise<void> {
    const handle = this.sectors.sectorApi().missions;
    const patch: Record<string, unknown> = {};
    if (this.canEdit(mission, 'notes') && this.editNotes !== (mission.notes ?? '')) patch['notes'] = this.editNotes;
    if (this.canEdit(mission, 'deadline') && this.editDeadline) patch['deadline'] = new Date(this.editDeadline);
    if (
      this.canEdit(mission, 'assignedSquadronId') &&
      this.editAssignedSquadronId &&
      this.editAssignedSquadronId !== mission.assignedSquadronId
    ) {
      patch['assignedSquadronId'] = this.editAssignedSquadronId;
    }
    if (this.canEdit(mission, 'fee') && this.editFee !== null) patch['fee'] = this.editFee;
    if (Object.keys(patch).length === 0) return;
    await handle.patch(handle.keyOf(mission), patch);
    this.missions.reload();
  }

  async remove(mission: Missions): Promise<void> {
    const handle = this.sectors.sectorApi().missions;
    await handle.remove(handle.keyOf(mission));
    this.selectedID.set(undefined);
    this.missions.reload();
  }

  async book(): Promise<void> {
    if (!this.newTitle || !this.newKind || !this.newClientId || !this.newDeadline) return;
    await this.sectors.sectorApi().missions.create({
      clientId: this.newClientId,
      kindId: this.newKind,
      title: this.newTitle,
      brief: this.newBrief || undefined,
      hazard: this.newHazard ?? 1,
      fee: this.newFee ?? 0,
      deadline: new Date(this.newDeadline),
      notes: this.newNotes || undefined,
    });
    this.newTitle = '';
    this.newBrief = '';
    this.newKind = '';
    this.newClientId = '';
    this.newHazard = null;
    this.newFee = null;
    this.newDeadline = '';
    this.newNotes = '';
    this.missions.reload();
  }

  async addSortie(mission: Missions): Promise<void> {
    if (!this.newSortieShipId || !this.newSortiePilot) return;
    await this.sectors.sectorApi().sorties.create({
      missionId: mission.id,
      shipId: this.newSortieShipId,
      pilotUserId: this.newSortiePilot,
      launchedAt: new Date(),
    });
    this.newSortieShipId = '';
    this.newSortiePilot = '';
    this.sorties.reload();
  }

  async addExpense(sortie: Sorties): Promise<void> {
    if (this.newExpenseAmount === null) return;
    await this.sectors.sectorApi().sortieExpenses.create({
      sortieId: sortie.id,
      category: this.newExpenseCategory,
      amount: this.newExpenseAmount,
    });
    this.newExpenseAmount = null;
    this.expenses.reload();
  }
}
