import { ApplicationRef } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ActivatedRoute, Router, convertToParamMap } from '@angular/router';
import { createApi } from '@app/service/zz_gen_api';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { IdleService } from '@cccteam/resource-angular/ui-idle-service';
import { LoginMethods, TransportRequest } from '@cccteam/resource';
import { ScriptedTransport, scriptedTransport } from '@cccteam/resource/testing';
import { environment } from '@env';
import { of } from 'rxjs';
import { LoginComponent } from './login.component';

/** What the portal outlet's methods route answers: the organization's directory, registered as directory. */
const METHODS: LoginMethods = {
  methods: [
    {
      name: 'directory',
      method: 'azure',
      kind: 'redirect',
      label: 'Directory',
      path: `${environment.apiUrl}/user/directory/login`,
    },
  ],
  pending: {
    status: `${environment.apiUrl}/user/pending`,
    confirm: `${environment.apiUrl}/user/pending/confirm`,
    cancel: `${environment.apiUrl}/user/pending/cancel`,
  },
};

// The page composes the library's login component: the component reads the methods route
// through the application's client and renders the directory's button from it, in the page's
// own words; the page ends no session and, on the component's signedIn, starts the
// idle watch and navigates. The redirect itself is the library's and is specified there.

describe('LoginComponent', () => {
  let fixture: ComponentFixture<LoginComponent>;
  let transport: ScriptedTransport;

  beforeEach(async () => {
    transport = scriptedTransport((request: TransportRequest) =>
      request.url.endsWith('/user/methods')
        ? { status: 200, body: METHODS, headers: { 'x-api-version': 'v0.3.1' } }
        : { status: 200, body: undefined },
    );
    await TestBed.configureTestingModule({
      imports: [LoginComponent],
      // Every request goes through the resource client on the scripted transport, which answers
      // the methods route with the outlet's list and everything else with an empty 200, and keeps
      // the log. provideResourceTesting also provides the empty router the component reads its
      // refusal code from.
      providers: [
        provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(LoginComponent);
    fixture.detectChanges();
    await TestBed.inject(ApplicationRef).whenStable();
    fixture.detectChanges();
  });

  it("shows the release the server named on the methods answer, in the card's footer", () => {
    const release: HTMLElement | null = fixture.nativeElement.querySelector('mat-card-footer.release ccc-release');
    expect(release?.textContent?.trim()).toBe('v0.3.1');
  });

  it('creates, ending no session, and the component asks the server what the outlet offers and whether a session stands', () => {
    expect(fixture.componentInstance).toBeTruthy();
    expect(transport.requests.map((request) => `${request.method} ${request.url}`)).toEqual([
      `GET ${environment.apiUrl}/user/methods`,
      `GET ${environment.apiUrl}/user/session`,
    ]);
  });

  it("offers the directory sign-in in the page's own words and says nothing while no refusal code is in the URL", () => {
    const element: HTMLElement = fixture.nativeElement;
    expect(element.querySelector('ccc-login-methods form')).toBeNull();
    const redirects = Array.from(element.querySelectorAll('button.ccc-login-redirect')).map((button) =>
      button.textContent?.trim(),
    );
    expect(redirects).toEqual(['Sign in with your organization']);
    expect(element.querySelector('.ccc-login-message')).toBeNull();
  });

  it("on the component's signedIn starts the idle watch and moves to the page it is handed", () => {
    const idle = TestBed.inject(IdleService);
    const started = vi.spyOn(idle, 'start');
    const navigated = vi.spyOn(TestBed.inject(Router), 'navigateByUrl').mockResolvedValue(true);
    fixture.componentInstance.go('/dashboard');
    expect(started).toHaveBeenCalledTimes(1);
    expect(navigated).toHaveBeenCalledWith('/dashboard');
    idle.stop();
  });
});

describe('LoginComponent, arriving with a mailed link', () => {
  // The page ends no session on arrival, so a mailed link followed signed in keeps the session:
  // an invitation is accepted by the person signed in, an address confirmation is posted for
  // the session the page finds, and a sign-up creates an account of its own. Signing out is
  // the sign-out route's.
  async function arriveWith(query: Record<string, string>): Promise<ScriptedTransport> {
    const params = convertToParamMap(query);
    const transport = scriptedTransport((request: TransportRequest) =>
      request.url.endsWith('/user/methods') ? { status: 200, body: METHODS } : { status: 200, body: undefined },
    );
    TestBed.resetTestingModule();
    await TestBed.configureTestingModule({
      imports: [LoginComponent],
      providers: [
        provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
        { provide: ActivatedRoute, useValue: { queryParamMap: of(params), snapshot: { queryParamMap: params } } },
      ],
    }).compileComponents();
    const fixture = TestBed.createComponent(LoginComponent);
    fixture.detectChanges();
    await TestBed.inject(ApplicationRef).whenStable();
    fixture.detectChanges();

    return transport;
  }

  it('keeps the session for every link: an invitation, an address confirmation, a sign-up', async () => {
    const links: Record<string, string>[] = [{ invitation: 'token' }, { verify: 'token' }, { signUp: 'token' }];
    for (const query of links) {
      const transport = await arriveWith(query);
      expect(transport.requests.filter((request) => request.method === 'DELETE')).toEqual([]);
    }
  });
});
