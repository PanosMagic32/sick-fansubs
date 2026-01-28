import { HttpClient } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { catchError, map, of } from 'rxjs';

import { WebConfigService } from '@web/shared';

export interface HealthStatus {
  status: 'ok' | 'error';
  timestamp?: string;
  uptime?: number;
}

@Injectable({ providedIn: 'root' })
export class HealthService {
  private readonly http = inject(HttpClient);
  private readonly webConfigService = inject(WebConfigService);

  checkHealth() {
    return this.http.get<HealthStatus>(`${this.webConfigService.API_URL}/health`).pipe(
      map((health) => health.status === 'ok'),
      catchError(() => of(false)),
    );
  }
}
