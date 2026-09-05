import { HttpClient } from '@angular/common/http';
import { computed, inject, Injectable, signal } from '@angular/core';
import { Router } from '@angular/router';
import { AuthService } from '@cccteam/ccc-lib/auth-service';
import { API_URL } from '@cccteam/ccc-lib/types';
import { firstValueFrom } from 'rxjs';

/** The impersonation record the session endpoint reports for a minted session. */
export interface ImpersonationRecord {
  actor: string;
  principalKind: 'User' | 'Role';
  principal: string;
  mask?: string[];
  reason?: string;
  expiresAt: string;
}

interface SessionResponse {
  authenticated: boolean;
  username: string;
  impersonation?: ImpersonationRecord;
}

/**
 * ImpersonationService drives the two impersonation moments (design plan §3): "view
 * as" mints a session that operates as another user under a List, Read mask, and
 * "act as a role" mints one that operates as a role with subject still bound to the
 * actor. The mint route is hand-written and gated by the ViewAsUser and AssumeRole
 * Execute registrations; the session endpoint reports the record back, which the
 * header renders as the persistent banner.
 */
@Injectable({ providedIn: 'root' })
export class ImpersonationService {
  private http = inject(HttpClient);
  private auth = inject(AuthService);
  private router = inject(Router);
  private apiUrl = inject(API_URL);

  /** The current session's impersonation record, if the session was minted. */
  readonly record = signal<ImpersonationRecord | undefined>(undefined);

  readonly banner = computed(() => {
    const record = this.record();
    if (!record) return undefined;
    if (record.principalKind === 'Role') {
      return { kind: 'Role' as const, text: `Acting as role ${record.principal}. You are ${record.actor}; subject still binds to you.` };
    }
    const mask = record.mask?.length ? `${record.mask.join(', ')} only` : 'unrestricted';
    return { kind: 'User' as const, text: `Viewing as ${record.principal}, ${mask}. You are ${record.actor}.` };
  });

  constructor() {
    void this.refresh();
  }

  /** Re-reads the session endpoint for the impersonation record. */
  async refresh(): Promise<void> {
    try {
      const session = await firstValueFrom(this.http.get<SessionResponse>(`${this.apiUrl}/user/session`));
      this.record.set(session.impersonation);
    } catch {
      this.record.set(undefined);
    }
  }

  /** Whether the session mask removes the permission (the service card's stripe). */
  masked(permission: string): boolean {
    const mask = this.record()?.mask;
    return !!mask?.length && !mask.includes(permission);
  }

  /** Mints a read-only session as another user and reloads the console as them. */
  async viewAs(user: string): Promise<void> {
    await this.mint({ kind: 'user', principal: user, reason: 'crew roster: view as' });
  }

  /** Mints a session that acts as a role and reloads the console under it. */
  async assumeRole(role: string): Promise<void> {
    await this.mint({ kind: 'role', principal: role, reason: 'crew roster: assume role' });
  }

  private async mint(body: { kind: 'user' | 'role'; principal: string; reason: string }): Promise<void> {
    await firstValueFrom(this.http.post(`${this.apiUrl}/impersonate`, body));
    // The minted session replaced the cookie: re-establish the client-side session and
    // its permission cache as the new principal, then start over at the dashboard.
    this.auth.permissions.clear();
    await firstValueFrom(this.auth.checkUserSession());
    await this.auth.permissions.refresh();
    await this.refresh();
    await this.router.navigateByUrl('/dashboard');
  }
}
