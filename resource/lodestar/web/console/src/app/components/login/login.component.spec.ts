import { ApplicationRef } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ActivatedRoute, Router, convertToParamMap } from '@angular/router';
import { createApi } from '@app/service/zz_gen_api';
import { LoginMethodsComponent } from '@cccteam/resource-angular/auth-login-methods';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { IdleService } from '@cccteam/resource-angular/ui-idle-service';
import { LoginMethods, TransportRequest } from '@cccteam/resource';
import { ScriptedTransport, scriptedTransport } from '@cccteam/resource/testing';
import { environment } from '@env';
import { of } from 'rxjs';
import { CREW_MANIFEST, DEMO_PASSWORD, LoginComponent } from './login.component';

/** What the console outlet's methods route answers: the password and the fleet's directory, in the generator program's order. */
const CONSOLE_METHODS: LoginMethods = {
  methods: [
    {
      name: 'password',
      method: 'password',
      kind: 'password',
      label: 'Password',
      path: '/console/api/user/password/login',
    },
    { name: 'google', method: 'google', kind: 'redirect', label: 'Google', path: '/console/api/user/google/login' },
  ],
  pending: {
    status: '/console/api/user/pending',
    confirm: '/console/api/user/pending/confirm',
    cancel: '/console/api/user/pending/cancel',
  },
};

// Demonstrates: client.login-component.
//
// The page composes the library's login component around the crew manifest: the component
// reads the methods route through the application's client and renders the form and the
// Google button from it; the page ends no session, prefills the component's form
// from a manifest card, and on the component's signedIn starts the idle watch and navigates.
// The sign-in itself (the XSRF re-read, the post, the pending step) is the library's and is
// specified there.

describe('LoginComponent', () => {
  let fixture: ComponentFixture<LoginComponent>;
  let transport: ScriptedTransport;

  beforeEach(async () => {
    transport = scriptedTransport((request: TransportRequest) =>
      request.url.endsWith('/user/methods')
        ? { status: 200, body: CONSOLE_METHODS, headers: { 'x-api-version': 'v0.3.1' } }
        : { status: 200, body: undefined },
    );
    await TestBed.configureTestingModule({
      imports: [LoginComponent],
      // Every request goes through the resource client on the scripted transport, which answers
      // the methods route with the console's list and everything else with an empty 200, and
      // keeps the log. provideResourceTesting also provides the empty router the component
      // reads its refusal code from.
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

  it('renders the password form and the Google button from the list, with the manifest beside them', () => {
    const element: HTMLElement = fixture.nativeElement;
    expect(element.querySelector('ccc-login-methods input[name="username"]')).not.toBeNull();
    expect(element.querySelector('ccc-login-methods input[name="password"]')).not.toBeNull();
    const redirects = Array.from(element.querySelectorAll('button.ccc-login-redirect')).map((button) =>
      button.textContent?.trim(),
    );
    expect(redirects).toEqual(['Sign in with Google']);
    expect(element.querySelectorAll('button.persona')).toHaveLength(CREW_MANIFEST.length);
    expect(element.querySelector('.ccc-login-message')).toBeNull();
  });

  it("prefills the component's form from the crew manifest, signing in staying an explicit click", () => {
    const component = fixture.componentInstance;
    expect(component.decks.length).toBeGreaterThan(1);
    expect(component.decks.flatMap((deck) => component.personasOn(deck))).toHaveLength(CREW_MANIFEST.length);

    component.fillPersona('overseer');
    fixture.detectChanges();
    const methods = fixture.debugElement.query((node) => node.componentInstance instanceof LoginMethodsComponent)
      .componentInstance as LoginMethodsComponent;
    expect(methods.username).toBe('overseer');
    expect(methods.password).toBe(DEMO_PASSWORD);
    expect(component.selected()).toBe('overseer');
    expect(fixture.nativeElement.querySelector('button.persona.selected .login')?.textContent?.trim()).toBe('overseer');
    expect(transport.requests.filter((request) => request.method === 'POST')).toHaveLength(0);
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
      request.url.endsWith('/user/methods') ? { status: 200, body: CONSOLE_METHODS } : { status: 200, body: undefined },
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
