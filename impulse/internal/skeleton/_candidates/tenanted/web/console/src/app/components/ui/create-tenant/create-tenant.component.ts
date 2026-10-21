import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { Component, inject, signal } from '@angular/core';
import { FormControl, FormGroup, ReactiveFormsModule, Validators } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { Router } from '@angular/router';
import { CreateTenant, createTenantRoute } from '@app/service/zz_gen_api';
import { TenantService } from '@app/tenant/tenant.service';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { environment } from '@env';
import { firstValueFrom } from 'rxjs';

/** What the create-your-tenant route answers: the new tenant's key. */
interface Created {
  id: string;
}

/**
 * The create-your-tenant page is the self-service half of the Tenant record's creation
 * shape (`@tenant(creation: both)`): a person who signed in and holds no tenant names
 * theirs here, and the generated route (`POST create-tenant`, the `createTenantRoute`
 * constant the generated client exports) writes the row, seats them as its administrator
 * and hands the tenant to the roster, so their next request stands in it. The dashboard
 * offers the page to a session without a tenant; a person who already holds one is
 * refused by the server, which the page reports in place. The route is no resource or
 * method of the client, so the page posts with the application's HttpClient, and once the
 * server answers it reloads what the session holds (the domains and every cached digest)
 * and selects the new tenant, so the header's picker and the dashboard show it at once.
 */
@Component({
  selector: 'app-create-tenant',
  imports: [MatButtonModule, MatCardModule, MatFormFieldModule, MatInputModule, ReactiveFormsModule],
  templateUrl: './create-tenant.component.html',
  styleUrl: './create-tenant.component.scss',
})
export class CreateTenantComponent {
  private http = inject(HttpClient);
  private auth = inject(AuthService);
  private router = inject(Router);
  private tenants = inject(TenantService);

  /** The record's fields: the key, a slug the tenant-scoped URLs carry, and the name. */
  readonly form = new FormGroup({
    id: new FormControl('', {
      nonNullable: true,
      validators: [Validators.required, Validators.pattern(/^[a-z0-9]+(-[a-z0-9]+)*$/)],
    }),
    name: new FormControl('', { nonNullable: true, validators: [Validators.required] }),
  });

  /** True while the post is in flight. */
  readonly busy = signal(false);
  /** The server's refusal, in its words, or empty. */
  readonly error = signal('');

  async create(): Promise<void> {
    if (this.form.invalid || this.busy()) {
      return;
    }
    this.busy.set(true);
    this.error.set('');
    const body: CreateTenant = this.form.getRawValue();
    try {
      const created = await firstValueFrom(this.http.post<Created>(`${environment.apiUrl}/${createTenantRoute}`, body));
      await firstValueFrom(this.auth.refreshPermissions());
      this.tenants.select(created.id);
      await this.router.navigate(['/dashboard']);
    } catch (failure) {
      this.error.set(refusal(failure));
    } finally {
      this.busy.set(false);
    }
  }
}

/** The sentence a failed post is reported with: the server's message where it gave one. */
function refusal(failure: unknown): string {
  if (failure instanceof HttpErrorResponse) {
    const message: unknown = failure.error?.message;
    return typeof message === 'string' && message ? message : failure.message;
  }
  return failure instanceof Error ? failure.message : String(failure);
}
