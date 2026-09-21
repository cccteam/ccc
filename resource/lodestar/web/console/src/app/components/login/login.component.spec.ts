import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { createApi } from '@app/service/zz_gen_api';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { ScriptedTransport, scriptedTransport } from '@cccteam/resource/testing';
import { environment } from '@env';
import { CREW_MANIFEST, DEMO_PASSWORD, LoginComponent } from './login.component';

describe('LoginComponent', () => {
  let fixture: ComponentFixture<LoginComponent>;
  let transport: ScriptedTransport;

  beforeEach(async () => {
    transport = scriptedTransport();
    await TestBed.configureTestingModule({
      imports: [LoginComponent],
      // The page posts the credentials through HttpClient, so the testing client stands in for
      // it; the session it ends on arrival goes through the resource client, on the scripted
      // transport, which answers every request with an empty 200 and keeps the log.
      providers: [
        provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
        provideHttpClient(),
        provideHttpClientTesting(),
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(LoginComponent);
    fixture.detectChanges();
  });

  it('creates, ending whatever session the browser arrived with', () => {
    expect(fixture.componentInstance).toBeTruthy();
    expect(transport.requests.map((request) => `${request.method} ${request.url}`)).toEqual([
      `DELETE ${environment.apiUrl}/user/session`,
    ]);
  });

  it('asks for a login and a password', () => {
    const element: HTMLElement = fixture.nativeElement;
    expect(element.querySelector('input[name="username"]')).not.toBeNull();
    expect(element.querySelector('input[name="password"]')).not.toBeNull();
    expect(element.querySelector('button[type="submit"]')).not.toBeNull();
  });

  it('prefills the form from the crew manifest, signing in staying an explicit click', () => {
    const component = fixture.componentInstance;
    expect(component.decks.length).toBeGreaterThan(1);
    expect(component.decks.flatMap((deck) => component.personasOn(deck))).toHaveLength(CREW_MANIFEST.length);

    component.fillPersona('overseer');
    expect(component.username).toBe('overseer');
    expect(component.password).toBe(DEMO_PASSWORD);
    expect(component.busy()).toBe(false);
  });
});
