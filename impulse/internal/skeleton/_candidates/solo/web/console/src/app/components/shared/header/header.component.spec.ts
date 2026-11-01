import { ComponentFixture, TestBed } from '@angular/core/testing';
import { MatDialog } from '@angular/material/dialog';
import { provideNoopAnimations } from '@angular/platform-browser/animations';
import { createApi } from '@app/service/zz_gen_api';
import { PermissionDigest, TransportRequest, TransportResponse } from '@cccteam/resource';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { ScriptedTransport, scriptedTransport } from '@cccteam/resource/testing';
import { environment } from '@env';
import { firstValueFrom } from 'rxjs';
import { HeaderComponent } from './header.component';

// The header shows the Feature flags link under List on FeatureFlags and opens the
// library's dialog from it; a person with no grant on the flags sees no link. The spec
// plays the server: the session, the digest, the domains and the flags.

/** The server as the spec plays it for one digest: signed in, with no tenant and no flag. */
function server(digest: PermissionDigest, request: TransportRequest): TransportResponse {
  const url = new URL(request.url, 'http://spec.test');
  switch (url.pathname) {
    case `${environment.apiUrl}/user/session`:
      return { status: 200, body: { authenticated: true, username: 'admin' } };
    case `${environment.apiUrl}/permission-digest`:
      return { status: 200, body: digest };
    case `${environment.apiUrl}/user-domains`:
      return { status: 200, body: [] };
    case `${environment.apiUrl}/features`:
      return { status: 200, body: { enabled: [] } };
    case `${environment.apiUrl}/feature-flags`:
      return { status: 200, body: [], headers: { 'total-count': '0' } };
    default:
      return { status: 404, body: { message: `the spec's server has no ${url.pathname}` } };
  }
}

/** The flags' reader: List on FeatureFlags, which shows the link. */
const flagsReader: PermissionDigest = { FeatureFlags: { List: 'granted', Read: 'granted' } };
/** Someone with no grant at all. */
const nobody: PermissionDigest = {};

describe('HeaderComponent', () => {
  let fixture: ComponentFixture<HeaderComponent>;
  let transport: ScriptedTransport;

  const element = (): HTMLElement => fixture.nativeElement as HTMLElement;
  const flagsButton = (): HTMLButtonElement | null => element().querySelector('button.flags');

  /** Signs in over the scripted server with the digest and builds the header. */
  const signIn = async (digest: PermissionDigest): Promise<void> => {
    transport = scriptedTransport((request) => server(digest, request));
    await TestBed.configureTestingModule({
      imports: [HeaderComponent],
      providers: [
        provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
        provideNoopAnimations(),
      ],
    }).compileComponents();
    await firstValueFrom(TestBed.inject(AuthService).checkUserSession());
    fixture = TestBed.createComponent(HeaderComponent);
    fixture.detectChanges();
  };

  afterEach(() => {
    TestBed.inject(MatDialog).closeAll();
  });

  it('creates with the brand, the menu, and the sign-out control', async () => {
    await signIn(nobody);
    expect(element().querySelector('a.brand')).not.toBeNull();
    expect(element().querySelector('app-topbar')).not.toBeNull();
    expect(element().querySelector('a[href="/logout"]')?.textContent).toContain('Sign out');
  });

  it('shows the Feature flags link under List on FeatureFlags and opens the dialog from it', async () => {
    await signIn(flagsReader);
    const button = flagsButton();
    expect(button?.textContent?.trim()).toBe('Feature flags');
    button?.click();
    fixture.detectChanges();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(document.querySelector('mat-dialog-container')).not.toBeNull();
  });

  it('shows no Feature flags link to a person with no grant on the flags', async () => {
    await signIn(nobody);
    expect(flagsButton()).toBeNull();
  });
});
