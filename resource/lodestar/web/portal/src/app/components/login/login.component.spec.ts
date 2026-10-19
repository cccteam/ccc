import { ApplicationRef } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { createApi } from '@app/service/zz_gen_api';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { IdleService } from '@cccteam/resource-angular/ui-idle-service';
import { LoginMethods, TransportRequest } from '@cccteam/resource';
import { ScriptedTransport, scriptedTransport } from '@cccteam/resource/testing';
import { environment } from '@env';
import { CLIENT_MANIFEST, LoginComponent } from './login.component';

/** What the portal outlet's methods route answers: the company directory alone. */
const PORTAL_METHODS: LoginMethods = {
  methods: [{ name: 'directory', method: 'google', kind: 'redirect', label: 'Company directory', path: '/portal/api/user/directory/login' }],
  pending: { status: '/portal/api/user/pending', confirm: '/portal/api/user/pending/confirm', cancel: '/portal/api/user/pending/cancel' },
};

// Demonstrates: client.login-component.
//
// The page composes the library's login component over the client manifest: the component
// reads the methods route through the application's client and renders the directory button
// from it, in the page's own words for the directory; the page ends the arriving session and,
// on the component's signedIn, starts the idle watch and navigates.

describe('LoginComponent', () => {
  let fixture: ComponentFixture<LoginComponent>;
  let transport: ScriptedTransport;

  beforeEach(async () => {
    transport = scriptedTransport((request: TransportRequest) =>
      request.url.endsWith('/user/methods') ? { status: 200, body: PORTAL_METHODS } : { status: 200, body: undefined },
    );
    await TestBed.configureTestingModule({
      imports: [LoginComponent],
      // Every request goes through the resource client on the scripted transport, which answers
      // the methods route with the portal's list and everything else with an empty 200, and keeps
      // the log. provideResourceTesting also provides the empty router the component reads its
      // refusal code from.
      providers: [provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) })],
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

  it('offers the directory sign-in the server listed, in the page\'s words, over the client manifest, and no password form', () => {
    const element: HTMLElement = fixture.nativeElement;
    const buttons = Array.from(element.querySelectorAll('button.ccc-login-redirect')).map((button) => button.textContent?.trim());
    expect(buttons).toEqual(['Sign in with your company directory']);
    expect(element.querySelector('input[name="username"]')).toBeNull();
    expect(element.querySelectorAll('.persona')).toHaveLength(CLIENT_MANIFEST.length);
    expect(element.querySelector('.ccc-login-message')).toBeNull();
  });

  it('on the component\'s signedIn starts the idle watch and moves to the page it is handed', () => {
    const idle = TestBed.inject(IdleService);
    const started = vi.spyOn(idle, 'start');
    const navigated = vi.spyOn(TestBed.inject(Router), 'navigateByUrl').mockResolvedValue(true);
    fixture.componentInstance.go('/tracker');
    expect(started).toHaveBeenCalledTimes(1);
    expect(navigated).toHaveBeenCalledWith('/tracker');
    idle.stop();
  });
});
