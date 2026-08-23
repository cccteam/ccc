import { HttpClient } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { Berths } from '@app/service/zz_gen_resources';
import { API_URL } from '@cccteam/ccc-lib/types';
import { Observable } from 'rxjs';

/**
 * StationsService talks to the station-scoped API surface by hand: the generated
 * TypeScript metadata does not carry domain routes yet, so the config-driven resource
 * components cannot address /api/stations/{stationID}/... — these pages do it
 * directly instead.
 */
@Injectable({ providedIn: 'root' })
export class StationsService {
  private http = inject(HttpClient);
  private apiUrl = inject(API_URL);

  stations(): Observable<{ stations: string[] }> {
    return this.http.get<{ stations: string[] }>(`${this.apiUrl}/stations`);
  }

  berths(station: string): Observable<Berths[]> {
    return this.http.get<Berths[]>(`${this.apiUrl}/stations/${station}/berths`);
  }

  createBerth(station: string, value: Partial<Berths>): Observable<unknown> {
    return this.http.patch(`${this.apiUrl}/stations/${station}/berths`, [{ op: 'add', path: '/', value }]);
  }

  patchBerth(station: string, id: string, value: Partial<Berths>): Observable<unknown> {
    return this.http.patch(`${this.apiUrl}/stations/${station}/berths`, [{ op: 'patch', path: `/${id}`, value }]);
  }

  removeBerth(station: string, id: string): Observable<unknown> {
    return this.http.patch(`${this.apiUrl}/stations/${station}/berths`, [{ op: 'remove', path: `/${id}` }]);
  }

  authorizeDocking(station: string, berthId: string, dockingCode: string): Observable<unknown> {
    return this.http.post(`${this.apiUrl}/stations/${station}/authorize-docking`, { berthId, dockingCode });
  }
}
