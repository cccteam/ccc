import { ComponentFixture, TestBed } from '@angular/core/testing';
import { MatSelect, MatSelectChange } from '@angular/material/select';
import { By } from '@angular/platform-browser';
import { createApi } from '@app/service/zz_gen_api';
import { TenantService } from '@app/tenant/tenant.service';
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

  it('creates with the brand, the menu, the tenant picker, and the sign-out control', () => {
    const element: HTMLElement = fixture.nativeElement;
    expect(element.querySelector('a.brand')).not.toBeNull();
    expect(element.querySelector('app-topbar')).not.toBeNull();
    expect(element.querySelector('mat-select')).not.toBeNull();
    expect(element.querySelector('button')?.textContent).toContain('Sign out');
  });

  it('choosing a tenant in the picker opens the same page under it, and sets no tenant itself', () => {
    const tenants = TestBed.inject(TenantService);
    const open = vi.spyOn(tenants, 'open').mockResolvedValue(true);
    const select: MatSelect = fixture.debugElement.query(By.directive(MatSelect)).componentInstance;

    select.selectionChange.emit(new MatSelectChange(select, 'beta'));

    expect(open).toHaveBeenCalledWith('beta');
    expect(tenants.current()).toBe('');
  });
});
