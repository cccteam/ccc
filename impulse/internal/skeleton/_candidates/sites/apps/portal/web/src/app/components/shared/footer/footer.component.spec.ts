import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ClientBase } from '@cccteam/resource';
import { scriptedTransport } from '@cccteam/resource/testing';
import { RESOURCE_CLIENT } from '@cccteam/resource-angular/resource-client';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { FooterComponent } from './footer.component';

describe('FooterComponent', () => {
  let fixture: ComponentFixture<FooterComponent>;

  beforeEach(async () => {
    // The footer's release line reads the client, whose every answer names the server's release.
    const transport = scriptedTransport({ status: 200, body: undefined, headers: { 'x-api-version': 'v0.3.1' } });
    await TestBed.configureTestingModule({
      imports: [FooterComponent],
      providers: [provideResourceTesting({ transport })],
    }).compileComponents();

    fixture = TestBed.createComponent(FooterComponent);
    fixture.detectChanges();
  });

  it('creates with the footer line', () => {
    expect(fixture.nativeElement.querySelector('footer.footer')?.textContent).toContain(
      'built on the cccteam resource stack',
    );
  });
  it('shows the release the server names once an answer has named it', async () => {
    expect(fixture.nativeElement.querySelector('ccc-release')?.textContent?.trim()).toBe('');
    await TestBed.inject<ClientBase>(RESOURCE_CLIENT).request('GET', 'user/session');
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('ccc-release')?.textContent?.trim()).toBe('v0.3.1');
  });
});
