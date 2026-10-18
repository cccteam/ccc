import { ApplicationRef } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { createApi } from '@app/service/zz_gen_api';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { IdleService } from '@cccteam/resource-angular/ui-idle-service';
import { LoginMethods, TransportRequest } from '@cccteam/resource';
import { ScriptedTransport, scriptedTransport } from '@cccteam/resource/testing';
import { environment } from '@env';
import { LoginComponent } from './login.component';

/** What the outlet's methods route answers: the password method the skeleton registers. */
const METHODS: LoginMethods = {
  methods: [
    {
      name: 'password',
      method: 'password',
      kind: 'password',
      label: 'Password',
      path: `${environment.apiUrl}/user/password/login`,
    },
  ],
  pending: {
    status: `${environment.apiUrl}/user/pending`,
    confirm: `${environment.apiUrl}/user/pending/confirm`,
    cancel: `${environment.apiUrl}/user/pending/cancel`,
  },
};

// The page composes the library's login component: the component reads the methods route
// through the application's client and renders the form from it; the page ends the arriving
// session and, on the component's signedIn, starts the idle watch and navigates. The sign-in
// itself (the XSRF re-read, the post, the pending step) is the library's and is specified there.

describe('LoginComponent', () => {
  let fixture: ComponentFixture<LoginComponent>;
  let transport: ScriptedTransport;

  beforeEach(async () => {
    transport = scriptedTransport((request: TransportRequest) =>
      request.url.endsWith('/user/methods') ? { status: 200, body: METHODS } : { status: 200, body: undefined },
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

  it('creates, ending whatever session the browser arrived with, and the component asks the server what the outlet offers', () => {
    expect(fixture.componentInstance).toBeTruthy();
    // The two arrival requests are independent: the logout waits on the live session's stop first.
    expect(transport.requests.map((request) => `${request.method} ${request.url}`).sort()).toEqual([
      `DELETE ${environment.apiUrl}/user/session`,
      `GET ${environment.apiUrl}/user/methods`,
    ]);
  });

  it('renders the password form from the list and says nothing while no refusal code is in the URL', () => {
    const element: HTMLElement = fixture.nativeElement;
    expect(element.querySelector('ccc-login-methods input[name="username"]')).not.toBeNull();
    expect(element.querySelector('ccc-login-methods input[name="password"]')).not.toBeNull();
    expect(element.querySelector('button.ccc-login-submit')?.textContent).toContain('Sign in');
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
