import { HttpClient } from '@angular/common/http';
import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { Router } from '@angular/router';
import { AuthService } from '@cccteam/ccc-lib/auth-service';
import { API_URL } from '@cccteam/ccc-lib/types';
import { IdleService } from '@cccteam/ccc-lib/ui-idle-service';

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
  { login: 'archivist', name: 'Archivist Ada', deck: 'Archive', proves: 'Finished missions everywhere; fees redacted until completed; the ship’s log.' },
  { login: 'hazards', name: 'Hazard Analyst Hale', deck: 'Hazard board', proves: 'The hazard board under a certification that expires.' },
  { login: 'dock', name: 'Dockmaster Dara', deck: 'Hangar deck', proves: 'The hangar deck on the day shift, 06:00 to 18:00 headquarters time.' },
  { login: 'watch', name: 'Night Watch Nadia', deck: 'Hangar deck', proves: 'The same deck on the night shift; weekday-only task notes.' },
];

@Component({
  selector: 'app-login',
  templateUrl: './login.component.html',
  styleUrls: ['./login.component.scss'],
  imports: [FormsModule, MatButtonModule, MatCardModule, MatFormFieldModule, MatIconModule, MatInputModule],
})
export class LoginComponent {
  private http = inject(HttpClient);
  private auth = inject(AuthService);
  private router = inject(Router);
  private idle = inject(IdleService);
  private apiUrl = inject(API_URL);

  username = '';
  password = '';
  busy = signal(false);

  readonly manifest = CREW_MANIFEST;
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
  }

  login(): void {
    if (this.busy()) {
      return;
    }
    this.busy.set(true);

    this.http.post(`${this.apiUrl}/user/login`, { username: this.username, password: this.password }).subscribe({
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

  private getAndResetRedirectUrl(): string {
    const redirectUrl = this.auth.redirectUrl();
    this.auth.redirectUrl.set('');
    if (redirectUrl === '' || redirectUrl.startsWith('/login')) {
      return '/dashboard';
    }
    return redirectUrl;
  }
}
