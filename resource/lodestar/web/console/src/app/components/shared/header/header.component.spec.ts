import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { createApi } from '@app/service/zz_gen_api';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { environment } from '@env';
import { HeaderComponent } from './header.component';

describe('HeaderComponent', () => {
  let fixture: ComponentFixture<HeaderComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [HeaderComponent],
      providers: [
        // The pilot card is a typed handle on the console's generated client; the impersonation
        // service posts through HttpClient.
        provideResourceTesting({ client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
        provideHttpClient(),
        provideHttpClientTesting(),
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(HeaderComponent);
    fixture.detectChanges();
  });

  it('creates with the brand, the menu, and the logout control, and no banner outside an impersonated session', () => {
    const element: HTMLElement = fixture.nativeElement;
    expect(element.querySelector('a.brand')?.textContent).toContain('Lodestar');
    expect(element.querySelector('app-topbar')).not.toBeNull();
    expect(Array.from(element.querySelectorAll('button')).map((button) => button.textContent?.trim())).toContain(
      'Logout',
    );
    expect(element.querySelector('.impersonation-banner')).toBeNull();
  });
});
