import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable } from 'rxjs';

export interface DashboardMetrics {
  totalUsers: number;
  activeUsers: number;
  suspendedUsers: number;
  adminCount: number;
  moderatorCount: number;
}

@Injectable({ providedIn: 'root' })
export class DashboardService {
  private readonly http = inject(HttpClient);

  /**
   * Fetch dashboard overview metrics.
   * Staff-only endpoint - role checked on back-end.
   */
  getMetrics(): Observable<DashboardMetrics> {
    return this.http.get<DashboardMetrics>('/api/admin/dashboard/metrics');
  }
}
