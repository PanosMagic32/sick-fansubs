/**
 * Admin dashboard — projects tab — the staff
 * project list at /admin/projects. Shares AdminListBase with the blog tab;
 * this leaf binds the projects data access, the slug secondary line, and
 * the projects form addresses.
 */

import { customElement } from "lit/decorators.js";

import { el } from "@shared/catalog/el.js";

import {
  listStaffProjects,
  type StaffProjectSummary,
} from "./data-access/projects-admin-api.js";
import {
  AdminListBase,
  type AdminListFilter,
  type AdminListPage,
} from "./ui/admin-list-base.js";

@customElement("admin-projects-page")
export class AdminProjectsPage extends AdminListBase {
  protected get listPath(): string {
    return "/admin/projects";
  }
  protected get createHref(): string {
    return "/admin/projects/new";
  }
  protected get editHrefPrefix(): string {
    return "/admin/projects/";
  }
  protected get createLabel(): string {
    return el.admin.createProject;
  }
  protected get emptyLabel(): string {
    return el.admin.listEmptyProjects;
  }
  protected get tab(): "projects" {
    return "projects";
  }

  protected async loadPage(
    after: string | null,
    limit: number,
    filter: AdminListFilter,
    signal: AbortSignal,
  ): Promise<AdminListPage> {
    const result = await listStaffProjects(
      {
        limit,
        ...(after ? { after } : {}),
        ...(filter.status ? { status: filter.status } : {}),
        ...(filter.q ? { q: filter.q } : {}),
      },
      { signal },
    );
    return {
      items: result.items.map(toRow),
      hasNext: result.pageInfo.hasNextPage,
      endCursor: result.pageInfo.endCursor,
      total: result.pageInfo.total,
    };
  }
}

/** The project row's secondary line is the slug — the admin-editable data
 * field the dashboard makes visible at a glance. The thumbnail and
 * description join it (the favorites card shape). */
function toRow(item: StaffProjectSummary) {
  return {
    id: item.id,
    title: item.title,
    status: item.status,
    updatedAt: item.updatedAt,
    secondary: item.slug,
    thumbnailUrl: item.thumbnailUrl,
    description: item.description,
  };
}

declare global {
  interface HTMLElementTagNameMap {
    "admin-projects-page": AdminProjectsPage;
  }
}
