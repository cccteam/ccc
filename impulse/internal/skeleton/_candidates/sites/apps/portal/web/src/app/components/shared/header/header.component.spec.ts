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
      providers: [provideResourceTesting({ client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) })],
    }).compileComponents();

    fixture = TestBed.createComponent(HeaderComponent);
    fixture.detectChanges();
  });

  it('creates with the brand, the menu, and the sign-out control', () => {
    const element: HTMLElement = fixture.nativeElement;
    expect(element.querySelector('a.brand')).not.toBeNull();
    expect(element.querySelector('app-topbar')).not.toBeNull();
    expect(element.querySelector('button')?.textContent).toContain('Sign out');
  });
});
