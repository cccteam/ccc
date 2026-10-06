import { DatePipe, DecimalPipe } from '@angular/common';
import { Component, computed, effect, inject, resource, signal, untracked } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { MatTableModule } from '@angular/material/table';
import { Router } from '@angular/router';
import { injectApi } from '@app/api/api';
import { DistressCalls as DistressCallFields, Methods, Permissions, Resources } from '@app/service/zz_gen_constants';
import { DistressCalls, Missions } from '@app/service/zz_gen_resources';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { storeSignal } from '@cccteam/resource-angular/resource-client';
import { Domain, Method, Page, ResourceHandle, rowCapabilities } from '@cccteam/resource';
import { IdleService } from '@cccteam/resource-angular/ui-idle-service';
import { tap } from 'rxjs';
import { TrackerGraphComponent } from './graph/tracker-graph.component';

/** One line of the company's statement, as app.clientStatementLine's wire struct. */
export interface ClientStatementLine {
  missionId: string;
  title: string;
  eventTime: string;
  eventSource: string;
  changeSet: Record<string, unknown> | null;
}

/**
 * The client's tracker: the missions their company booked in the selected sector, paged
 * at the size the portal's descriptor carries and each drawn as the same state graph
 * the flight deck uses but with no edge lit except Stand down; a three-field
 * distress-call form (summary, severity, and the contact details, the one PII field a
 * client writes); the calls they filed listed back; the company's statement over the
 * change log (the portal-only manual resource); and their own contact record in the
 * header. Everything renders from the portal's own generated client, whose digest and
 * user-domains channels live under /portal/api.
 *
 * Demonstrates: outlet.session, typescript.second-target, paging.descriptor-sizes, create-form-narrowing, @manualAddResource.outlet.
 */
@Component({
  selector: 'app-tracker',
  imports: [
    DatePipe,
    DecimalPipe,
    FormsModule,
    MatButtonModule,
    MatCardModule,
    MatFormFieldModule,
    MatInputModule,
    MatSelectModule,
    MatTableModule,
    TrackerGraphComponent,
  ],
  templateUrl: './tracker.component.html',
  styleUrl: './tracker.component.scss',
})
export class TrackerComponent {
  private api = injectApi();
  private auth = inject(AuthService);
  private router = inject(Router);
  private idle = inject(IdleService);
  private permissions = storeSignal(this.api.permissions.snapshot);

  readonly resources = Resources;
  readonly methods = Methods;
  readonly fields = { ...DistressCallFields.fieldName, ...DistressCallFields.piiFieldName };
  readonly username = this.auth.sessionInfo;

  // The sectors where the client's portal role is held, from the portal's user-domains.
  sectors = computed(() => [...this.auth.domains()]);
  current = signal<string>('');

  constructor() {
    effect(() => {
      const sectors = this.sectors();
      if (!this.current() && sectors[0]) {
        untracked(() => this.current.set(sectors[0]!));
      }
    });
    effect(() => {
      const sector = this.current();
      if (!sector) return;
      untracked(() => void this.api.permissions.loadDigest(sector as Domain).catch(() => undefined));
    });
  }

  private sectorApi = computed(() => (this.current() ? this.api.domain(this.current()) : undefined));

  // Own record: the portal's ClientContacts Read grant is `userId = subject`.
  contact = resource({
    params: () => {
      this.permissions();
      return { ok: this.api.clientContacts.can(Permissions.List) };
    },
    loader: ({ params }) => (params.ok ? this.api.clientContacts.list() : Promise.resolve([])),
    defaultValue: [],
  });

  // The tracker pages at the descriptor's default for Missions; next and previous
  // follow the server's Link relations.
  readonly pageSize = this.api.descriptor.resources[Resources.Missions]?.page?.default;
  missionsPage = resource({
    params: () => {
      this.permissions();
      const api = this.sectorApi();
      return { handle: api?.missions.can(Permissions.List) ? api.missions : undefined };
    },
    loader: ({ params }) =>
      params.handle
        ? params.handle.page({ sort: { field: 'deadline' }, limit: this.pageSize, count: true, capabilities: ['Execute'] })
        : Promise.resolve(undefined as Page<Missions> | undefined),
  });
  missions = computed(() => this.missionsPage.value()?.rows ?? []);
  missionTotal = computed(() => this.missionsPage.value()?.total);

  /** Steps to the neighboring page the server named; the total from the first page stays shown. */
  async turnPage(direction: 'next' | 'prev'): Promise<void> {
    const page = this.missionsPage.value();
    const step = direction === 'next' ? page?.next : page?.prev;
    if (!step) return;
    const total = page?.total;
    const turned = await step();
    this.missionsPage.set({ ...turned, total: turned.total ?? total });
  }

  /**
   * The company's statement: a manual resource registered on the portal outlet alone
   * (no struct, no generated handle), described once per sector through the client's
   * escape hatch. The route scopes it to the caller's company.
   */
  private statementHandle(sector: string): ResourceHandle<ClientStatementLine, [], 'list'> {
    return this.api.define<ClientStatementLine, [], 'list'>(
      {
        resource: Resources.ClientStatements,
        property: 'clientStatements',
        route: 'client-statements',
        scope: 'domain',
        consolidated: false,
        keys: [],
        operations: ['list'],
      },
      sector as Domain,
    );
  }

  statement = resource({
    params: () => {
      this.permissions();
      const sector = this.current();
      return { sector, ok: !!sector && this.api.can(Permissions.List, Resources.ClientStatements, sector as Domain) };
    },
    loader: ({ params }) => (params.ok ? this.statementHandle(params.sector).list() : Promise.resolve([])),
    defaultValue: [] as ClientStatementLine[],
  });

  calls = resource({
    params: () => {
      this.permissions();
      const api = this.sectorApi();
      return { handle: api?.distressCalls.can(Permissions.List) ? api.distressCalls : undefined };
    },
    loader: ({ params }) => (params.handle ? params.handle.list() : Promise.resolve([])),
    defaultValue: [] as DistressCalls[],
  });

  canFile = computed(() => {
    this.permissions();
    return this.api.can(Permissions.Create, Resources.DistressCalls, (this.current() || undefined) as Domain | undefined);
  });

  creatableFields = computed(() => {
    this.permissions();
    return this.api.grantedFields(Permissions.Create, Resources.DistressCalls, (this.current() || undefined) as Domain | undefined);
  });

  canWrite(field: string): boolean {
    const fields = this.creatableFields();
    return fields === undefined || fields.includes(field);
  }

  selectedID = signal<string | undefined>(undefined);
  selected = computed(() => this.missions().find((m) => m.id === this.selectedID()));

  newSummary = '';
  newSeverity: number | null = null;
  newCallerContact = '';

  select(mission: Missions): void {
    this.selectedID.set(this.selectedID() === mission.id ? undefined : mission.id);
  }

  executable(mission: Missions): Method[] {
    return (rowCapabilities(mission)?.Execute ?? []) as Method[];
  }

  contactName(): string {
    return this.contact.value()[0]?.displayName ?? this.username().username;
  }

  async fire(mission: Missions, method: Method): Promise<void> {
    const api = this.sectorApi();
    if (!api || method !== Methods.StandDownMission) return;
    await api.standDownMission.execute({ missionId: mission.id });
    this.missionsPage.reload();
    this.statement.reload();
  }

  async file(): Promise<void> {
    const api = this.sectorApi();
    if (!api || !this.newSummary || this.newSeverity === null) return;
    await api.distressCalls.create({
      summary: this.newSummary,
      severity: this.newSeverity,
      ...(this.canWrite(this.fields.callerContact) ? { callerContact: this.newCallerContact } : {}),
    });
    this.newSummary = '';
    this.newSeverity = null;
    this.newCallerContact = '';
    this.calls.reload();
  }

  logout(): void {
    this.idle.stop();
    this.auth
      .logout()
      .pipe(tap(() => this.router.navigate(['/login'])))
      .subscribe();
  }
}
