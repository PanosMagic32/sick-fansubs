import { httpResource, HttpResourceRef } from '@angular/common/http';
import { Injectable, Signal, inject } from '@angular/core';

import type { UserRole, UserStatus } from '@shared/types';
import { WebConfigService } from '@web/shared';

export interface DashboardMetrics {
  totalUsers: number;
  activeUsers: number;
  suspendedUsers: number;
  adminCount: number;
  moderatorCount: number;
}

export interface DashboardUser {
  id: string;
  username: string;
  email: string;
  avatar?: string;
  role: UserRole;
  status: UserStatus;
  createdAt?: string;
  updatedAt?: string;
}

export interface DashboardUsersManagementResponse {
  users: DashboardUser[];
  count: number;
  page: number;
  pageSize: number;
}

@Injectable({ providedIn: 'root' })
export class DashboardService {
  private readonly webConfigService = inject(WebConfigService);

  getUsers(): HttpResourceRef<DashboardUser[] | undefined> {
    return httpResource<DashboardUser[]>(() => ({
      url: `${this.webConfigService.API_URL}/user`,
    }));
  }

  getManagementUsers(params: {
    page: Signal<number>;
    pageSize: Signal<number>;
    search: Signal<string>;
    role: Signal<'all' | UserRole>;
    status: Signal<'all' | UserStatus>;
    sortBy: Signal<'username' | 'email' | 'role' | 'status' | 'createdAt' | 'updatedAt'>;
    sortDirection: Signal<'asc' | 'desc'>;
  }): HttpResourceRef<DashboardUsersManagementResponse | undefined> {
    return httpResource<DashboardUsersManagementResponse>(() => {
      const page = params.page();
      const pageSize = params.pageSize();
      const search = params.search().trim();
      const role = params.role();
      const status = params.status();
      const sortBy = params.sortBy();
      const sortDirection = params.sortDirection();

      const query = new URLSearchParams({
        page: String(page),
        pageSize: String(pageSize),
        sortBy,
        sortDirection,
      });

      if (search.length > 0) {
        query.set('search', search);
      }

      if (role !== 'all') {
        query.set('role', role);
      }

      if (status !== 'all') {
        query.set('status', status);
      }

      return {
        url: `${this.webConfigService.API_URL}/user/management?${query.toString()}`,
      };
    });
  }
}
