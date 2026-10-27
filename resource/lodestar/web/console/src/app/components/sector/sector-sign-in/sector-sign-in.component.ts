import { Component, computed, input, resource, signal } from '@angular/core';
import { FormControl, FormGroup, ReactiveFormsModule, Validators } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { MatSlideToggleModule } from '@angular/material/slide-toggle';
import { MatTableModule } from '@angular/material/table';
import { injectApi } from '@app/api/api';
import { Permissions, Resources } from '@app/service/zz_gen_constants';
import { CrewSectorRoles, SectorDirectories, Sectors } from '@app/service/zz_gen_resources';
import { ApiError, Domain } from '@cccteam/resource';

/** A sign-in method a directory is reached through, with what its connection is. */
export interface DirectoryMethod {
  value: string;
  label: string;
  connection: string;
  example: string;
}

/** A Google Workspace, named by its domain: the form's first choice. */
const googleWorkspace: DirectoryMethod = {
  value: 'google',
  label: 'Google Workspace',
  connection: "the Workspace's domain",
  example: 'example',
};

/**
 * The methods a directory is reached through, as the session library spells them, each
 * with the connection a sector names it by: the value the session's first auth event
 * records when a person signs in through it.
 */
export const directoryMethods: readonly DirectoryMethod[] = [
  googleWorkspace,
  { value: 'azure', label: 'Microsoft Entra', connection: "the Entra tenant's id", example: '6f2c1b7e-3d4a-4c1e-9b2a-1f0e8d7c6b5a' },
  { value: 'workos', label: 'WorkOS', connection: "the connection's id", example: 'conn_01HXYZ' },
];

/**
 * The sector's sign-in page: who may enter the sector, as its declaration says. The
 * declaration is two things on the Sector record: the directories the sector admits its
 * people through (the SectorDirectories rows under the sector: a sign-in method and the
 * connection its identities carry), and whether its people enter through those only
 * (the record's directory-only flag, which closes passwords). A directory is declared or
 * withdrawn here under the sector's Create and Delete grants on SectorDirectories, which
 * the sector's marshal holds; the flag is set under the Update grant on the global
 * Sectors record, which headquarters holds. Each declared directory carries a default
 * seat, the role a stranger who signs in through it is seated with in the sector at their
 * first sign-in: the marshal picks it from the sector's roles, or none, under the Update
 * grant on the row, and a row with none tells the directory's strangers the sector admits
 * them by invitation only. The server reloads what every instance knows after each write,
 * so a change decides the next request at once, and each write leaves a change event.
 *
 * Demonstrates: tenancy.declaration, tenancy.directory-only, tenancy.default-seat.
 */
@Component({
  selector: 'app-sector-sign-in',
  imports: [
    MatButtonModule,
    MatCardModule,
    MatFormFieldModule,
    MatInputModule,
    MatSelectModule,
    MatSlideToggleModule,
    MatTableModule,
    ReactiveFormsModule,
  ],
  templateUrl: './sector-sign-in.component.html',
  styleUrl: './sector-sign-in.component.scss',
})
export class SectorSignInComponent {
  private api = injectApi();

  /** The sector the URL names, bound from the route. */
  sectorID = input.required<string>();

  readonly methods = directoryMethods;
  readonly columns = ['method', 'connection', 'seat', 'withdraw'];
  /** The picker's choice for a directory whose strangers are admitted by invitation only. */
  readonly invitationOnly = '';

  /** The sector's row on the global record: its name and its directory-only flag. */
  sector = resource({
    params: () => ({ sector: this.sectorID() }),
    loader: ({ params }): Promise<Sectors | undefined> =>
      this.api.sectors.can(Permissions.Read) ? this.api.sectors.read([params.sector]) : Promise.resolve(undefined),
  });

  /** The directories the sector declares, listed under the sector where the digest grants List. */
  directories = resource({
    params: () => ({ sector: this.sectorID() }),
    loader: ({ params }): Promise<SectorDirectories[]> => {
      const handle = this.api.domain(params.sector as Domain).sectorDirectories;
      return handle.can(Permissions.List) ? handle.list() : Promise.resolve([]);
    },
    defaultValue: [],
  });

  /** The sector's roles, the default seat's choices, listed where the digest grants List on them. */
  roles = resource({
    params: () => ({ sector: this.sectorID() }),
    loader: ({ params }): Promise<CrewSectorRoles[]> => {
      const handle = this.api.domain(params.sector as Domain).crewSectorRoles;
      return handle.can(Permissions.List) ? handle.list() : Promise.resolve([]);
    },
    defaultValue: [],
  });

  canList = computed(() => this.api.domain(this.sectorID() as Domain).sectorDirectories.can(Permissions.List));
  canSeat = computed(() => this.api.domain(this.sectorID() as Domain).sectorDirectories.can(Permissions.Update));
  canDeclare = computed(() => this.api.domain(this.sectorID() as Domain).sectorDirectories.can(Permissions.Create));
  canWithdraw = computed(() => this.api.domain(this.sectorID() as Domain).sectorDirectories.can(Permissions.Delete));
  canClose = computed(() => this.api.can(Permissions.Update, Resources.Sectors));

  /** The directory being declared. */
  readonly form = new FormGroup({
    method: new FormControl(googleWorkspace.value, { nonNullable: true, validators: [Validators.required] }),
    connection: new FormControl('', { nonNullable: true, validators: [Validators.required] }),
  });

  /** True while a write is in flight. */
  readonly busy = signal(false);
  /** The server's refusal of the last write, in its words, or empty. */
  readonly error = signal('');

  /** The method the form has chosen, for the connection's hint. */
  chosen = computed(() => this.methods.find((m) => m.value === this.method()) ?? googleWorkspace);
  private method = signal(googleWorkspace.value);

  constructor() {
    this.form.controls.method.valueChanges.subscribe((value) => this.method.set(value));
  }

  labelOf(method: string): string {
    return this.methods.find((m) => m.value === method)?.label ?? method;
  }

  async declare(): Promise<void> {
    if (this.form.invalid || this.busy()) {
      return;
    }
    const { method, connection } = this.form.getRawValue();
    await this.write(async () => {
      await this.api.domain(this.sectorID() as Domain).sectorDirectories.create({ method, connection: connection.trim() });
      this.form.controls.connection.reset();
      this.directories.reload();
    });
  }

  /** What the directory's strangers get, as the row says. */
  seatOf(directory: SectorDirectories): string {
    return directory.defaultSeat || 'Invitation only';
  }

  /** Sets the directory's default seat: a role of the sector, or none (null clears the column). */
  async seat(directory: SectorDirectories, role: string): Promise<void> {
    await this.write(async () => {
      await this.api.domain(this.sectorID() as Domain).sectorDirectories.patch([directory.id], { defaultSeat: role || null });
      this.directories.reload();
    });
  }

  async withdraw(directory: SectorDirectories): Promise<void> {
    await this.write(async () => {
      await this.api.domain(this.sectorID() as Domain).sectorDirectories.remove([directory.id]);
      this.directories.reload();
    });
  }

  async close(directoryOnly: boolean): Promise<void> {
    await this.write(async () => {
      await this.api.sectors.patch([this.sectorID()], { directoryOnly });
      this.sector.reload();
    });
  }

  private async write(act: () => Promise<void>): Promise<void> {
    this.busy.set(true);
    this.error.set('');
    try {
      await act();
    } catch (failure) {
      this.error.set(refusal(failure));
    } finally {
      this.busy.set(false);
    }
  }
}

/** The sentence a failed write is reported with: the server's message where it gave one. */
function refusal(failure: unknown): string {
  if (failure instanceof ApiError) {
    return `${failure.status}: ${failure.message}`;
  }
  return failure instanceof Error ? failure.message : String(failure);
}
