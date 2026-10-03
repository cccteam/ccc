import { ComponentFixture, TestBed } from '@angular/core/testing';
import { createApi } from '@app/service/zz_gen_api';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { ScriptedTransport, scriptedTransport } from '@cccteam/resource/testing';
import { environment } from '@env';
import { CLIENT_MANIFEST, LoginComponent } from './login.component';

describe('LoginComponent', () => {
  let fixture: ComponentFixture<LoginComponent>;
  let transport: ScriptedTransport;

  beforeEach(async () => {
    transport = scriptedTransport();
    await TestBed.configureTestingModule({
      imports: [LoginComponent],
      // The directory sign-in sends the browser away; nothing is posted from here. The session the
      // page ends on arrival goes through the resource client, on the scripted transport, which
      // answers every request with an empty 200 and keeps the log. provideResourceTesting also
      // provides the empty router the page reads its refusal code from.
      providers: [
        provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
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

  it('offers the directory sign-in over the client manifest and says nothing while no refusal code is in the URL', () => {
    const element: HTMLElement = fixture.nativeElement;
    expect(element.querySelector('button')?.textContent).toContain('Sign in through your company directory');
    expect(element.querySelectorAll('.persona')).toHaveLength(CLIENT_MANIFEST.length);
    expect(element.querySelector('p.error')).toBeNull();
  });
});
