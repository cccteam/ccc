/** Demonstrates: login-manifest, auth.password, auth.multi-method, auth.login-methods, auth.login-refusal-code, client.login-component. */
import { Component, inject, viewChild } from '@angular/core';
import { MatCardModule } from '@angular/material/card';
import { MatIconModule } from '@angular/material/icon';
import { Router } from '@angular/router';
import { LoginMethodsComponent } from '@cccteam/resource-angular/auth-login-methods';
import { IdleService } from '@cccteam/resource-angular/ui-idle-service';
import { ReleaseComponent } from '@cccteam/resource-angular/ui-release';

/** One card on the crew manifest: the login is the job word, the name alliterates. */
interface Persona {
  login: string;
  name: string;
  deck: string;
  proves: string;
}

/** The demo password every persona shares (seeded by cmd/bootstrap, committed deliberately). */
export const DEMO_PASSWORD = 'lodestar';

/**
 * The crew manifest, grouped by deck. Every persona in design plan §4 except the
 * droid, which has no login. Selecting a card prefills the form; signing in stays an
 * explicit click — switching personas is the comparison tool, never more than two
 * clicks.
 */
export const CREW_MANIFEST: Persona[] = [
  {
    login: 'governor',
    name: 'Governor Greer',
    deck: 'Headquarters',
    proves: 'Every global role, marshal in all three sectors: the pure-RBAC baseline.',
  },
  {
    login: 'marshal',
    name: 'Marshal Maren',
    deck: 'Headquarters',
    proves: 'Full authority at Anvil, nothing at Bastion or Cinder; every transition including Scrap.',
  },
  {
    login: 'cadet',
    name: 'Cadet Cass',
    deck: 'Flight deck',
    proves: 'Sees hazard 1 and 2 only; Claim lit on those alone; two-input call form.',
  },
  {
    login: 'pilot',
    name: 'Pilot Pax',
    deck: 'Flight deck',
    proves: 'Clearance 3 and certifications decide the board; no quarantine-bay ships.',
  },
  {
    login: 'veteran',
    name: 'Veteran Vela',
    deck: 'Flight deck',
    proves: 'Routine, low-fee missions never reach her board (NOT over an OR).',
  },
  {
    login: 'lead',
    name: 'Flight Lead Lior',
    deck: 'Flight deck',
    proves: "Hammer's missions; runs launch, hold, resume, complete; sorties while underway.",
  },
  {
    login: 'dispatcher',
    name: 'Dispatcher Dunn',
    deck: 'Flight deck',
    proves: 'Assigns own squadrons on open or claimed missions; extends deadlines, never pulls them in.',
  },
  {
    login: 'overseer',
    name: 'Overseer Orla',
    deck: 'Flight deck',
    proves: 'The overdue desk: reassign only after the deadline passes — watch it flip.',
  },
  {
    login: 'booking',
    name: 'Booking Agent Bex',
    deck: 'Client desk',
    proves: 'Books within the fee limit; stands down own unclaimed bookings.',
  },
  {
    login: 'wingco',
    name: 'Wing Commander Wilde',
    deck: 'Flight deck',
    proves: "Forge Wing's squadrons and the high-hazard desk.",
  },
  {
    login: 'engineer',
    name: 'Engineer Ezra',
    deck: 'Hangar deck',
    proves: 'Inspect, begin, flight-test, pass or fail; estimates after inspection; hails ships.',
  },
  {
    login: 'quartermaster',
    name: 'Quartermaster Quill',
    deck: 'Hangar deck',
    proves: 'Books sortie expenses only while the mission is underway (two hops deep).',
  },
  {
    login: 'supercargo',
    name: 'Supercargo Sol',
    deck: 'Salvage hold',
    proves: 'Releases bonded cargo once; disposes of expired bond.',
  },
  {
    login: 'salvor',
    name: 'Salvor Sable',
    deck: 'Salvage hold',
    proves: 'Recovers wrecks for outfits whose salvage cover is not refused; undecided outfits still qualify.',
  },
  {
    login: 'yeoman',
    name: 'Yeoman Yael',
    deck: 'Headquarters',
    proves: 'Reads the standing orders: a key-less list served whole, sorted on request, never paged.',
  },
  {
    login: 'purser',
    name: 'Purser Priya',
    deck: 'Client desk',
    proves: "Reads each mission's expense manifest and downloads it as a CSV sheet rendered on request.",
  },
  {
    login: 'registrar',
    name: 'Registrar Rhea',
    deck: 'Archive',
    proves:
      'Keeps the document register: retitles, replaces, and deletes mission documents; a replaced or deleted file leaves the store with the commit.',
  },
  {
    login: 'archivist',
    name: 'Archivist Ada',
    deck: 'Archive',
    proves: 'Finished missions everywhere; fees redacted until completed; the ship’s log.',
  },
  {
    login: 'assessor',
    name: 'Assessor Asa',
    deck: 'Hazard board',
    proves: 'Prices cover on open missions: sees every title, and the hazard level only while a mission is still open.',
  },
  {
    login: 'hazards',
    name: 'Hazard Analyst Hale',
    deck: 'Hazard board',
    proves: 'The hazard board under a certification that expires.',
  },
  {
    login: 'dock',
    name: 'Dockmaster Dara',
    deck: 'Hangar deck',
    proves: 'The hangar deck on the day shift, 06:00 to 18:00 headquarters time.',
  },
  {
    login: 'watch',
    name: 'Night Watch Nadia',
    deck: 'Hangar deck',
    proves: 'The same deck on the night shift; weekday-only task notes.',
  },
  {
    login: 'harbormaster',
    name: 'Harbormaster Hollis',
    deck: 'Hangar deck',
    proves:
      'Watches the fleet board live: a refit or a hail shows up without a reload, and a board reopened within minutes needs no request.',
  },
  {
    login: 'adjutant',
    name: 'Adjutant Alba',
    deck: 'Headquarters',
    proves:
      'Keeps the commendations desk, which is behind a feature flag: turns it on and off at runtime, and every instance follows.',
  },
  {
    login: 'surveyor',
    name: 'Surveyor Shen',
    deck: 'Headquarters',
    proves:
      'Charts new sectors: a sector she creates is served at once, on every instance, with no restart, migration or new login.',
  },
];

/**
 * The crew's second way in. Navigator Nova is on no manifest and has no password: the
 * fleet's Google directory signs her in, provisions her under her verified email at the
 * first sign-in, and her roles are the directory's groups (crew-pilot names Pilot). In
 * development the session library's skipAuth build simulates the directory from
 * APP_GOOGLE_USERNAME and APP_GOOGLE_ROLES in .envrc.
 */
export const DIRECTORY_PERSONA = {
  login: 'navigator',
  name: 'Navigator Nova',
  proves:
    'Signs in through the fleet’s directory, no password: provisioned at her first sign-in, seated by her groups at every one.',
};

/**
 * The console's login page composes the library's login component around the crew manifest.
 * The component asks the outlet's methods route what the crew auth offers, in the order the
 * generator program registered the methods, and renders it: the password form for the
 * password method, posted to the path the route names after the list is read again (the
 * read refreshes the XSRF cookie the post needs), and a button for the Google directory,
 * a redirect it sends the browser to with the page to return to. A refused or waiting
 * redirect sign-in comes back here with the reason as a code in the query (?code=) or the
 * step in ?pending=, never text; the component shows the sentence the library holds for
 * the code, nothing for a code it does not know, and the confirmation form for a step that
 * waits. The page keeps what is its own: the manifest that prefills the form, the
 * development notes, and where a sign-in lands.
 */
@Component({
  selector: 'app-login',
  templateUrl: './login.component.html',
  styleUrls: ['./login.component.scss'],
  imports: [LoginMethodsComponent, MatCardModule, MatIconModule, ReleaseComponent],
})
export class LoginComponent {
  private router = inject(Router);
  private idle = inject(IdleService);

  /** The library's login component, whose form the manifest prefills. */
  readonly methods = viewChild.required(LoginMethodsComponent);

  readonly manifest = CREW_MANIFEST;
  readonly directoryPersona = DIRECTORY_PERSONA;
  readonly decks = [...new Set(CREW_MANIFEST.map((p) => p.deck))];

  personasOn(deck: string): Persona[] {
    return this.manifest.filter((p) => p.deck === deck);
  }

  /** Prefills the component's form with the persona; signing in stays an explicit click. */
  fillPersona(login: string): void {
    const methods = this.methods();
    methods.username = login;
    methods.password = DEMO_PASSWORD;
  }

  /** The login the form holds, for the manifest's selected card. */
  selected(): string {
    return this.methods().username;
  }

  /**
   * The page ends no session: the component's first request, the methods list, primes the
   * XSRF cookie the sign-in's post needs, and a session the browser arrives with is the
   * component's to offer Continue or Sign out, or to accept a mailed link with. Signing out
   * is the sign-out route's.
   */
  constructor() {
    this.idle.stop();
  }

  /** Signed in and the session checked: the idle watch starts and the browser moves to the page the guard turned away from, else the dashboard. */
  go(page: string): void {
    this.idle.start();
    void this.router.navigateByUrl(page);
  }
}
