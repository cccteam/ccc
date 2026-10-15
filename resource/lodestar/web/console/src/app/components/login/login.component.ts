/** Demonstrates: login-manifest, auth.password, auth.multi-method, auth.login-methods, auth.login-refusal-code. */
import { DOCUMENT } from '@angular/common';
import { HttpClient } from '@angular/common/http';
import { Component, computed, inject, signal } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { ActivatedRoute, Router } from '@angular/router';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { API_URL } from '@cccteam/resource-angular/types';
import { UiCoreService } from '@cccteam/resource-angular/ui-core-service';
import { IdleService } from '@cccteam/resource-angular/ui-idle-service';
import { map } from 'rxjs';

/** One card on the crew manifest: the login is the job word, the name alliterates. */
interface Persona {
  login: string;
  name: string;
  deck: string;
  proves: string;
}

/**
 * One sign-in method the outlet offers, as the server's methods route describes it: the
 * registration's name, the library method behind it, how the page drives it (a password
 * form, or a redirect that sends the browser away), the label to show, and the path the
 * router mounted it at. The page names no path of its own.
 */
export interface LoginMethod {
  name: string;
  method: string;
  kind: 'password' | 'redirect';
  label: string;
  path: string;
}

/** What the methods route answers: the methods in the order they were registered, and the pending sign-in's routes. */
export interface LoginMethods {
  methods: LoginMethod[];
  pending?: { status: string; confirm: string; cancel: string };
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
  { login: 'governor', name: 'Governor Greer', deck: 'Headquarters', proves: 'Every global role, marshal in all three sectors: the pure-RBAC baseline.' },
  { login: 'marshal', name: 'Marshal Maren', deck: 'Headquarters', proves: 'Full authority at Anvil, nothing at Bastion or Cinder; every transition including Scrap.' },
  { login: 'cadet', name: 'Cadet Cass', deck: 'Flight deck', proves: 'Sees hazard 1 and 2 only; Claim lit on those alone; two-input call form.' },
  { login: 'pilot', name: 'Pilot Pax', deck: 'Flight deck', proves: 'Clearance 3 and certifications decide the board; no quarantine-bay ships.' },
  { login: 'veteran', name: 'Veteran Vela', deck: 'Flight deck', proves: 'Routine, low-fee missions never reach her board (NOT over an OR).' },
  { login: 'lead', name: 'Flight Lead Lior', deck: 'Flight deck', proves: "Hammer's missions; runs launch, hold, resume, complete; sorties while underway." },
  { login: 'dispatcher', name: 'Dispatcher Dunn', deck: 'Flight deck', proves: 'Assigns own squadrons on open or claimed missions; extends deadlines, never pulls them in.' },
  { login: 'overseer', name: 'Overseer Orla', deck: 'Flight deck', proves: 'The overdue desk: reassign only after the deadline passes — watch it flip.' },
  { login: 'booking', name: 'Booking Agent Bex', deck: 'Client desk', proves: 'Books within the fee limit; stands down own unclaimed bookings.' },
  { login: 'wingco', name: 'Wing Commander Wilde', deck: 'Flight deck', proves: "Forge Wing's squadrons and the high-hazard desk." },
  { login: 'engineer', name: 'Engineer Ezra', deck: 'Hangar deck', proves: 'Inspect, begin, flight-test, pass or fail; estimates after inspection; hails ships.' },
  { login: 'quartermaster', name: 'Quartermaster Quill', deck: 'Hangar deck', proves: 'Books sortie expenses only while the mission is underway (two hops deep).' },
  { login: 'supercargo', name: 'Supercargo Sol', deck: 'Salvage hold', proves: 'Releases bonded cargo once; disposes of expired bond.' },
  { login: 'salvor', name: 'Salvor Sable', deck: 'Salvage hold', proves: 'Recovers wrecks for outfits whose salvage cover is not refused; undecided outfits still qualify.' },
  { login: 'yeoman', name: 'Yeoman Yael', deck: 'Headquarters', proves: 'Reads the standing orders: a key-less list served whole, sorted on request, never paged.' },
  { login: 'purser', name: 'Purser Priya', deck: 'Client desk', proves: "Reads each mission's expense manifest and downloads it as a CSV sheet rendered on request." },
  { login: 'registrar', name: 'Registrar Rhea', deck: 'Archive', proves: 'Keeps the document register: retitles, replaces, and deletes mission documents; a replaced or deleted file leaves the store with the commit.' },
  { login: 'archivist', name: 'Archivist Ada', deck: 'Archive', proves: 'Finished missions everywhere; fees redacted until completed; the ship’s log.' },
  { login: 'assessor', name: 'Assessor Asa', deck: 'Hazard board', proves: 'Prices cover on open missions: sees every title, and the hazard level only while a mission is still open.' },
  { login: 'hazards', name: 'Hazard Analyst Hale', deck: 'Hazard board', proves: 'The hazard board under a certification that expires.' },
  { login: 'dock', name: 'Dockmaster Dara', deck: 'Hangar deck', proves: 'The hangar deck on the day shift, 06:00 to 18:00 headquarters time.' },
  { login: 'watch', name: 'Night Watch Nadia', deck: 'Hangar deck', proves: 'The same deck on the night shift; weekday-only task notes.' },
  { login: 'harbormaster', name: 'Harbormaster Hollis', deck: 'Hangar deck', proves: "Watches the fleet board live: a refit or a hail shows up without a reload, and a board reopened within minutes needs no request." },
  { login: 'adjutant', name: 'Adjutant Alba', deck: 'Headquarters', proves: 'Keeps the commendations desk, which is behind a feature flag: turns it on and off at runtime, and every instance follows.' },
  { login: 'surveyor', name: 'Surveyor Shen', deck: 'Headquarters', proves: 'Charts new sectors: a sector she creates is served at once, on every instance, with no restart, migration or new login.' },
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
  proves: 'Signs in through the fleet’s directory, no password: provisioned at her first sign-in, seated by her groups at every one.',
};

/**
 * The console's login page renders from what the server tells it. On arrival it asks the
 * outlet's methods route what the crew auth offers, in the order the generator program
 * registered the methods: the password, which it drives as a form posted to the path the
 * route names, and the Google directory, a redirect it sends the browser to with the
 * page to return to. A refused or waiting redirect sign-in comes back here with the
 * reason as a code in the query (?code=), never text; the page shows the sentence it
 * holds for the code and nothing for a code it does not know.
 */
@Component({
  selector: 'app-login',
  templateUrl: './login.component.html',
  styleUrls: ['./login.component.scss'],
  imports: [FormsModule, MatButtonModule, MatCardModule, MatFormFieldModule, MatIconModule, MatInputModule],
})
export class LoginComponent {
  private http = inject(HttpClient);
  private auth = inject(AuthService);
  private ui = inject(UiCoreService);
  private router = inject(Router);
  private route = inject(ActivatedRoute);
  private idle = inject(IdleService);
  private apiUrl = inject(API_URL);
  private document = inject(DOCUMENT);

  username = '';
  password = '';
  busy = signal(false);

  /** The methods the outlet offers, as the server listed them; empty until the route answers. */
  methods = signal<LoginMethod[]>([]);
  /** The password form's method, when the outlet offers one. */
  passwordMethod = computed(() => this.methods().find((method) => method.kind === 'password'));
  /** The redirect sign-ins, one button each, in the server's order. */
  redirectMethods = computed(() => this.methods().filter((method) => method.kind === 'redirect'));

  /** The refusal code a refused redirect sign-in came back with, or empty: the only thing the URL says. */
  code = toSignal(this.route.queryParamMap.pipe(map((params) => params.get('code') ?? '')), {
    initialValue: '',
  });
  /** The sentence this page holds for the code, from the library's login messages; nothing for a code it does not know. */
  message = computed(() => this.ui.loginMessage(this.code()));

  readonly manifest = CREW_MANIFEST;
  readonly directoryPersona = DIRECTORY_PERSONA;
  readonly decks = [...new Set(CREW_MANIFEST.map((p) => p.deck))];

  personasOn(deck: string): Persona[] {
    return this.manifest.filter((p) => p.deck === deck);
  }

  fillPersona(login: string): void {
    this.username = login;
    this.password = DEMO_PASSWORD;
  }

  constructor() {
    this.auth.logout().subscribe();
    this.idle.stop();
    this.http.get<LoginMethods>(`${this.apiUrl}/user/methods`).subscribe((list) => this.methods.set(list.methods));
  }

  /** Posts the form to the password method's path and, signed in, moves to the page the guard turned away from. */
  login(): void {
    const method = this.passwordMethod();
    if (this.busy() || !method) {
      return;
    }
    this.busy.set(true);

    this.http.post(method.path, { username: this.username, password: this.password }).subscribe({
      next: () => {
        this.auth.checkUserSession().subscribe({
          next: () => {
            this.idle.start();
            this.router.navigateByUrl(this.getAndResetRedirectUrl());
          },
          complete: () => this.busy.set(false),
        });
      },
      error: () => this.busy.set(false),
    });
  }

  /** Sends the browser to a redirect method's path, naming the page the callback returns to. */
  signInThrough(method: LoginMethod): void {
    const returnUrl = encodeURIComponent(this.returnPage(this.getAndResetRedirectUrl()));
    this.document.location.assign(`${method.path}?returnUrl=${returnUrl}`);
  }

  /** The browser path of an application page, resolved against the document's base (the console's mount). */
  private returnPage(page: string): string {
    return new URL(page.replace(/^\//, ''), this.document.baseURI).pathname;
  }

  private getAndResetRedirectUrl(): string {
    const redirectUrl = this.auth.redirectUrl();
    this.auth.redirectUrl.set('');
    if (redirectUrl === '' || redirectUrl.startsWith('/login')) {
      return '/dashboard';
    }
    return redirectUrl;
  }
}
