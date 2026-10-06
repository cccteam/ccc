import { ComponentFixture, TestBed } from '@angular/core/testing';
import { createApi } from '@app/service/zz_gen_api';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { environment } from '@env';
import { UiComponent } from './ui.component';

describe('UiComponent', () => {
  let fixture: ComponentFixture<UiComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [UiComponent],
      providers: [provideResourceTesting({ client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) })],
    }).compileComponents();

    fixture = TestBed.createComponent(UiComponent);
    fixture.detectChanges();
  });

  it('creates the shell: the header above the routed page, the footer below it', () => {
    const element: HTMLElement = fixture.nativeElement;
    expect(element.querySelector('app-header')).not.toBeNull();
    expect(element.querySelector('router-outlet')).not.toBeNull();
    expect(element.querySelector('app-footer')).not.toBeNull();
  });
});
