import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { createApi, createTenantRoute } from '@app/service/zz_gen_api';
import { TenantService } from '@app/tenant/tenant.service';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { TransportRequest, TransportResponse } from '@cccteam/resource';
import { ScriptedTransport, scriptedTransport } from '@cccteam/resource/testing';
import { environment } from '@env';
import { firstValueFrom } from 'rxjs';
import { CreateTenantComponent } from './create-tenant.component';

/**
 * The server as the spec plays it through the client's transport: a session holding the
 * tenants listed, with an empty digest everywhere. The post itself goes through HttpClient,
 * which the testing controller answers below.
 */
function server(domains: () => string[]): (request: TransportRequest) => TransportResponse {
  return (request) => {
    const url = new URL(request.url, 'http://skeleton.test');
    switch (url.pathname) {
      case `${environment.apiUrl}/user/session`:
        return { status: 200, body: { authenticated: true, username: 'newcomer' } };
      case `${environment.apiUrl}/permission-digest`:
        return { status: 200, body: {} };
      case `${environment.apiUrl}/user-domains`:
        return { status: 200, body: domains() };
      case `${environment.apiUrl}/features`:
        return { status: 200, body: { enabled: [] } };
      default:
        return { status: 404, body: { message: `unscripted ${request.method} ${request.url}` } };
    }
  };
}

describe('CreateTenantComponent', () => {
  let fixture: ComponentFixture<CreateTenantComponent>;
  let transport: ScriptedTransport;
  let http: HttpTestingController;
  let domains: string[];

  beforeEach(async () => {
    domains = [];
    transport = scriptedTransport(server(() => domains));
    await TestBed.configureTestingModule({
      imports: [CreateTenantComponent],
      providers: [
        provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
        provideHttpClient(),
        provideHttpClientTesting(),
      ],
    }).compileComponents();
    await firstValueFrom(TestBed.inject(AuthService).checkUserSession());
    http = TestBed.inject(HttpTestingController);
    fixture = TestBed.createComponent(CreateTenantComponent);
    fixture.detectChanges();
  });

  afterEach(() => http.verify());

  it('renders the form for a session holding no tenant, submittable once the key and the name are filled', () => {
    const element: HTMLElement = fixture.nativeElement;
    expect(element.querySelector('h1')?.textContent).toContain('Create your tenant');
    const submit = element.querySelector<HTMLButtonElement>('button[type="submit"]');
    expect(submit?.disabled).toBe(true);

    fixture.componentInstance.form.setValue({ id: 'West', name: 'West' });
    fixture.detectChanges();
    expect(fixture.componentInstance.form.controls.id.hasError('pattern')).toBe(true);
    expect(submit?.disabled).toBe(true);

    fixture.componentInstance.form.setValue({ id: 'west', name: 'West' });
    fixture.detectChanges();
    expect(submit?.disabled).toBe(false);
  });

  it('posts the record to the generated route, reloads what the session holds, selects the tenant and moves to the dashboard', async () => {
    const navigated = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
    fixture.componentInstance.form.setValue({ id: 'west', name: 'West' });
    const pending = fixture.componentInstance.create();

    const request = http.expectOne(`${environment.apiUrl}/${createTenantRoute}`);
    expect(request.request.method).toBe('POST');
    expect(request.request.body).toEqual({ id: 'west', name: 'West' });
    // The server has the tenant now: the reload lists it.
    domains = ['west'];
    request.flush({ id: 'west' });
    await pending;

    expect(TestBed.inject(AuthService).domains()).toEqual(['west']);
    expect(TestBed.inject(TenantService).current()).toBe('west');
    expect(navigated).toHaveBeenCalledWith(['/dashboard']);
    expect(fixture.componentInstance.error()).toBe('');
  });

  it("reports the server's refusal in place and stays", async () => {
    const navigated = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
    fixture.componentInstance.form.setValue({ id: 'west', name: 'West' });
    const pending = fixture.componentInstance.create();

    http
      .expectOne(`${environment.apiUrl}/${createTenantRoute}`)
      .flush(
        { message: 'the session already holds a tenant; a person creates one tenant and becomes its administrator' },
        { status: 403, statusText: 'Forbidden' },
      );
    await pending;
    fixture.detectChanges();

    expect(fixture.componentInstance.error()).toBe(
      'the session already holds a tenant; a person creates one tenant and becomes its administrator',
    );
    expect((fixture.nativeElement as HTMLElement).querySelector('.refusal')?.textContent).toContain('already holds a tenant');
    expect(navigated).not.toHaveBeenCalled();
    expect(TestBed.inject(TenantService).current()).toBe('');
  });
});
