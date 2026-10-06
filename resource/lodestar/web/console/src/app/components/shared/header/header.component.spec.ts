// Demonstrates: @feature.field, @feature.
import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
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
// library's dialog from it, which lists the one flag Lodestar declares; a persona with no
// grant on the flags sees no link. The pilot card's citation count, the card's one gated
// field, shows while the commendations flag is on and is absent while it is off.

/** One persona as the spec's server plays them: the session, the digest, the enabled flags, and the card. */
interface Persona {
  username: string;
  digest: PermissionDigest;
  enabled: string[];
  cards: Record<string, unknown>[];
}

/** The crew's List grant on the cards, field by field as the digest carries it, with the gated field when the flag is on. */
function cardDigest(gated: boolean): PermissionDigest {
  const digest: PermissionDigest = { PilotCards: { List: 'granted' } };
  for (const field of ['displayName', 'clearance', 'feeLimit', 'certifications', 'squadrons', ...(gated ? ['commendations'] : [])]) {
    digest[`PilotCards.${field}`] = { List: 'granted' };
  }
  return digest;
}

const adjutant: Persona = {
  username: 'adjutant',
  digest: { FeatureFlags: { List: 'granted', Read: 'granted' }, SetFeature: { Execute: 'granted' } },
  enabled: [],
  cards: [],
};

const cadet: Persona = {
  username: 'cadet',
  digest: cardDigest(false),
  enabled: [],
  cards: [{ userId: 'cadet', displayName: 'Cadet Cass', clearance: 1, feeLimit: 0, certifications: [], squadrons: [] }],
};

/** Pax, whose two citations the desk holds: his card with the flag on and with it off. */
const paxCited: Persona = {
  username: 'pilot',
  digest: cardDigest(true),
  enabled: ['commendations'],
  cards: [{ userId: 'pilot', displayName: 'Pilot Pax', clearance: 3, feeLimit: 0, certifications: ['hazard-3'], squadrons: ['Hammer'], commendations: 2 }],
};

const paxDark: Persona = {
  ...paxCited,
  digest: cardDigest(false),
  enabled: [],
  cards: [{ userId: 'pilot', displayName: 'Pilot Pax', clearance: 3, feeLimit: 0, certifications: ['hazard-3'], squadrons: ['Hammer'] }],
};

/** The one flag Lodestar declares, off, as the flags resource lists it. */
const flagRows = [
  {
    name: 'commendations',
    description: 'Commendations lets headquarters cite a pilot for a sortie flown well.',
    enabled: false,
    updatedAt: '2026-10-01T12:00:00Z',
    updatedBy: 'MigrateFeatures',
  },
];

function server(persona: Persona, request: TransportRequest): TransportResponse {
  const url = new URL(request.url, 'http://lodestar.test');
  switch (url.pathname) {
    case `${environment.apiUrl}/user/session`:
      return { status: 200, body: { authenticated: true, username: persona.username } };
    case `${environment.apiUrl}/permission-digest`:
      return { status: 200, body: persona.digest };
    case `${environment.apiUrl}/user-domains`:
      return { status: 200, body: [] };
    case `${environment.apiUrl}/features`:
      return { status: 200, body: { enabled: persona.enabled } };
    case `${environment.apiUrl}/feature-flags`:
      return { status: 200, body: flagRows, headers: { 'total-count': String(flagRows.length) } };
    case `${environment.apiUrl}/pilot-cards`: {
      // As the server does: the named columns alone, and the gated field is unknown while the flag is off.
      const columns = url.searchParams.get('columns')?.split(',') ?? [];
      if (columns.includes('commendations') && !persona.enabled.includes('commendations')) {
        return { status: 400, body: { message: 'unknown column: commendations' } };
      }
      const rows = persona.cards.map((card) =>
        columns.length ? Object.fromEntries(Object.entries(card).filter(([key]) => columns.includes(key))) : card,
      );
      return { status: 200, body: rows, headers: { 'total-count': String(rows.length) } };
    }
    default:
      return { status: 404, body: { message: `unscripted ${request.method} ${request.url}` } };
  }
}

describe('HeaderComponent', () => {
  let fixture: ComponentFixture<HeaderComponent>;
  let transport: ScriptedTransport;

  /** Runs change detection and lets pending promises land until `done` answers true, for at most `rounds` rounds. */
  const settle = async (done: () => boolean, rounds = 50): Promise<void> => {
    for (let i = 0; i < rounds && !done(); i++) {
      fixture.detectChanges();
      await new Promise((resolve) => setTimeout(resolve, 0));
    }
    fixture.detectChanges();
  };

  const element = (): HTMLElement => fixture.nativeElement as HTMLElement;
  const buttons = (): string[] =>
    Array.from(element().querySelectorAll('button')).map((button) => button.textContent?.trim() ?? '');
  const cardText = (): string => element().querySelector('.card')?.textContent?.replace(/\s+/g, ' ').trim() ?? '';

  /** Signs the persona in over the scripted server (the session, the digest, the domains, the flags) and builds the header. */
  const signIn = async (persona: Persona): Promise<void> => {
    transport = scriptedTransport((request) => server(persona, request));
    await TestBed.configureTestingModule({
      imports: [HeaderComponent],
      providers: [
        // The pilot card is a typed handle on the console's generated client; the impersonation
        // service posts through HttpClient; the flags dialog opens in the Material overlay.
        provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
        provideHttpClient(),
        provideHttpClientTesting(),
        provideNoopAnimations(),
      ],
    }).compileComponents();
    await firstValueFrom(TestBed.inject(AuthService).checkUserSession());
    fixture = TestBed.createComponent(HeaderComponent);
    fixture.detectChanges();
    await settle(() => persona.cards.length === 0 || cardText() !== '');
  };

  afterEach(() => {
    TestBed.inject(MatDialog).closeAll();
  });

  it('creates with the brand, the menu, and the logout control, and no banner outside an impersonated session', async () => {
    await signIn(cadet);
    expect(element().querySelector('a.brand')?.textContent).toContain('Lodestar');
    expect(element().querySelector('app-topbar')).not.toBeNull();
    expect(buttons()).toContain('Logout');
    expect(element().querySelector('.impersonation-banner')).toBeNull();
  });

  const links: { name: string; persona: Persona; shown: boolean }[] = [
    { name: 'the adjutant, holding List on FeatureFlags, has the Feature flags link', persona: adjutant, shown: true },
    { name: 'the cadet, with no grant on the flags, has no Feature flags link', persona: cadet, shown: false },
  ];
  for (const tt of links) {
    it(tt.name, async () => {
      await signIn(tt.persona);
      expect(buttons().includes('Feature flags')).toBe(tt.shown);
    });
  }

  it("the link opens the library's dialog, which lists the commendations flag off, read from the flags resource", async () => {
    await signIn(adjutant);
    const link = Array.from(element().querySelectorAll('button')).find((button) => button.textContent?.includes('Feature flags'));
    expect(link).toBeDefined();
    link?.click();
    const dialog = (): HTMLElement | null => document.querySelector('ccc-feature-flags-dialog');
    await settle(() => dialog()?.querySelector('li.feature-flag') !== null && dialog()?.querySelector('li.feature-flag') !== undefined);
    const rows = Array.from(dialog()?.querySelectorAll('li.feature-flag') ?? []);
    expect(rows.map((row) => row.querySelector('.feature-flag-name')?.textContent?.trim())).toEqual(['commendations']);
    expect(rows[0]?.textContent).toContain('Commendations lets headquarters cite a pilot');
    expect(dialog()?.querySelector('button[role="switch"]')?.getAttribute('aria-checked')).toBe('false');
    expect(transport.requests.map((request) => request.url)).toContain(`${environment.apiUrl}/feature-flags?sort=name&limit=all`);
  });

  /** The columns each pilot-cards request named, in request order. */
  const cardColumns = (): (string | null)[] =>
    transport.requests
      .map((request) => new URL(request.url, 'http://lodestar.test'))
      .filter((url) => url.pathname === `${environment.apiUrl}/pilot-cards`)
      .map((url) => url.searchParams.get('columns'));

  const cards: { name: string; persona: Persona; want: string; absent?: string; columns: string }[] = [
    {
      name: "with the commendations flag on, the card asks for the gated column the digest grants and counts Pax's citations",
      persona: paxCited,
      want: 'Cited: 2',
      columns: 'userId,certifications,clearance,commendations,displayName,feeLimit,squadrons',
    },
    {
      name: 'with the flag off, the card asks for the columns without it and the line is absent',
      persona: paxDark,
      want: 'Pilot Pax · clearance 3 · Hammer',
      absent: 'Cited',
      columns: 'userId,certifications,clearance,displayName,feeLimit,squadrons',
    },
  ];
  for (const tt of cards) {
    it(tt.name, async () => {
      await signIn(tt.persona);
      expect(cardText()).toContain(tt.want);
      if (tt.absent) {
        expect(cardText()).not.toContain(tt.absent);
      }
      expect(cardColumns()).toEqual([tt.columns]);
    });
  }
});
