import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { createApi } from '@app/service/zz_gen_api';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { ScriptedTransport, scriptedTransport } from '@cccteam/resource/testing';
import { environment } from '@env';
import { CREW_MANIFEST, DEMO_PASSWORD, LoginComponent, LoginMethod, LoginMethods } from './login.component';

/** What the console outlet's methods route answers: the password and the fleet's directory, in the generator program's order. */
const PASSWORD_METHOD: LoginMethod = { name: 'password', method: 'password', kind: 'password', label: 'Password', path: '/console/api/user/password/login' };
const GOOGLE_METHOD: LoginMethod = { name: 'google', method: 'google', kind: 'redirect', label: 'Google', path: '/console/api/user/google/login' };
const CONSOLE_METHODS: LoginMethods = {
  methods: [PASSWORD_METHOD, GOOGLE_METHOD],
  pending: { status: '/console/api/user/pending', confirm: '/console/api/user/pending/confirm', cancel: '/console/api/user/pending/cancel' },
};

describe('LoginComponent', () => {
  let fixture: ComponentFixture<LoginComponent>;
  let transport: ScriptedTransport;
  let http: HttpTestingController;

  beforeEach(async () => {
    transport = scriptedTransport();
    await TestBed.configureTestingModule({
      imports: [LoginComponent],
      // The page reads the methods route and posts the credentials through HttpClient, so the
      // testing client stands in for it; the session it ends on arrival goes through the resource
      // client, on the scripted transport, which answers every request with an empty 200 and keeps
      // the log. provideResourceTesting also provides the empty router the page reads its refusal
      // code from.
      providers: [
        provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
        provideHttpClient(),
        provideHttpClientTesting(),
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(LoginComponent);
    http = TestBed.inject(HttpTestingController);
    fixture.detectChanges();
  });

  afterEach(() => http.verify());

  /** Answers the methods route the way the console outlet does; the page asks it under the API URL the application provides. */
  function answerMethods(list: LoginMethods = CONSOLE_METHODS): void {
    const request = http.expectOne((candidate) => candidate.url.endsWith('/user/methods'));
    expect(request.request.method).toBe('GET');
    request.flush(list);
    fixture.detectChanges();
  }

  it('creates, ending whatever session the browser arrived with, and asks the server what the outlet offers', () => {
    expect(fixture.componentInstance).toBeTruthy();
    expect(transport.requests.map((request) => `${request.method} ${request.url}`)).toEqual([
      `DELETE ${environment.apiUrl}/user/session`,
    ]);
    answerMethods();
    expect(fixture.componentInstance.methods()).toEqual(CONSOLE_METHODS.methods);
  });

  it('renders from the list: the password form for the password method and a button for each redirect method, in the server order', () => {
    const element: HTMLElement = fixture.nativeElement;
    expect(element.querySelector('form')).toBeNull();
    answerMethods();
    expect(element.querySelector('input[name="username"]')).not.toBeNull();
    expect(element.querySelector('input[name="password"]')).not.toBeNull();
    expect(element.querySelector('button[type="submit"]')).not.toBeNull();
    const redirects = Array.from(element.querySelectorAll('button.redirect')).map((button) => button.textContent?.trim());
    expect(redirects).toEqual(['Sign in with Google']);
    expect(element.querySelector('p.error')).toBeNull();
  });

  it('shows no password form when the outlet offers none', () => {
    answerMethods({ methods: [GOOGLE_METHOD] });
    const element: HTMLElement = fixture.nativeElement;
    expect(element.querySelector('form')).toBeNull();
    expect(element.querySelectorAll('button.redirect')).toHaveLength(1);
  });

  it('ends the attempt when the refreshed list no longer offers a password sign-in', () => {
    answerMethods();
    const component = fixture.componentInstance;
    component.fillPersona('overseer');
    component.login();
    answerMethods({ methods: [GOOGLE_METHOD] });
    http.expectNone('/console/api/user/password/login');
    expect(component.busy()).toBe(false);
    expect(fixture.nativeElement.querySelector('form')).toBeNull();
  });

  it('refreshes the XSRF token with a request, then posts the credentials to the path the server named for the password method', () => {
    answerMethods();
    const component = fixture.componentInstance;
    component.fillPersona('overseer');
    component.login();
    // The sign-in's POST must echo the auth's XSRF cookie, which a page left open may have
    // outlived: the page reads the list again first, and the answer refreshes the cookie.
    http.expectNone('/console/api/user/password/login');
    answerMethods();
    const request = http.expectOne('/console/api/user/password/login');
    expect(request.request.method).toBe('POST');
    expect(request.request.body).toEqual({ username: 'overseer', password: DEMO_PASSWORD });
    expect(component.busy()).toBe(true);
    request.flush(null, { status: 401, statusText: 'Unauthorized' });
    expect(component.busy()).toBe(false);
  });

  it('prefills the form from the crew manifest, signing in staying an explicit click', () => {
    answerMethods();
    const component = fixture.componentInstance;
    expect(component.decks.length).toBeGreaterThan(1);
    expect(component.decks.flatMap((deck) => component.personasOn(deck))).toHaveLength(CREW_MANIFEST.length);

    component.fillPersona('overseer');
    expect(component.username).toBe('overseer');
    expect(component.password).toBe(DEMO_PASSWORD);
    expect(component.busy()).toBe(false);
  });
});
