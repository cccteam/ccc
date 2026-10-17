import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { createApi } from '@app/service/zz_gen_api';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { ScriptedTransport, scriptedTransport } from '@cccteam/resource/testing';
import { environment } from '@env';
import { CLIENT_MANIFEST, LoginComponent, LoginMethods } from './login.component';

/** What the portal outlet's methods route answers: the company directory alone. */
const PORTAL_METHODS: LoginMethods = {
  methods: [{ name: 'directory', method: 'google', kind: 'redirect', label: 'Company directory', path: '/portal/api/user/directory/login' }],
  pending: { status: '/portal/api/user/pending', confirm: '/portal/api/user/pending/confirm', cancel: '/portal/api/user/pending/cancel' },
};

describe('LoginComponent', () => {
  let fixture: ComponentFixture<LoginComponent>;
  let transport: ScriptedTransport;
  let http: HttpTestingController;

  beforeEach(async () => {
    transport = scriptedTransport();
    await TestBed.configureTestingModule({
      imports: [LoginComponent],
      // The page reads the methods route through HttpClient, so the testing client stands in for
      // it; the directory sign-in then sends the browser away, and nothing is posted from here.
      // The session the page ends on arrival goes through the resource client, on the scripted
      // transport, which answers every request with an empty 200 and keeps the log.
      // provideResourceTesting also provides the empty router the page reads its refusal code from.
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

  it('creates, ending whatever session the browser arrived with, and asks the server what the outlet offers', () => {
    expect(fixture.componentInstance).toBeTruthy();
    expect(transport.requests.map((request) => `${request.method} ${request.url}`)).toEqual([
      `DELETE ${environment.apiUrl}/user/session`,
    ]);
    http.expectOne((candidate) => candidate.url.endsWith('/user/methods')).flush(PORTAL_METHODS);
    expect(fixture.componentInstance.methods()).toEqual(PORTAL_METHODS.methods);
  });

  it('offers the directory sign-in the server listed over the client manifest and says nothing while no refusal code is in the URL', () => {
    const element: HTMLElement = fixture.nativeElement;
    expect(element.querySelector('button')).toBeNull();
    http.expectOne((candidate) => candidate.url.endsWith('/user/methods')).flush(PORTAL_METHODS);
    fixture.detectChanges();
    expect(element.querySelector('button')?.textContent).toContain('Sign in through your company directory');
    expect(element.querySelectorAll('.persona')).toHaveLength(CLIENT_MANIFEST.length);
    expect(element.querySelector('p.error')).toBeNull();
  });
});
