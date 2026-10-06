/**
 * Admin dashboard — blog tab — the staff blog list at /admin. The shared
 * guards, tab chrome, list/pager markup, and states live in AdminListBase;
 * this leaf binds the blog data access and addresses. Rows render the
 * favorites card shape (thumbnail + description).
 */

import { customElement } from "lit/decorators.js";

import { el } from "@shared/catalog/el.js";

import {
  listStaffBlogPosts,
  type StaffBlogPostSummary,
} from "./data-access/blog-admin-api.js";
import {
  AdminListBase,
  type AdminListFilter,
  type AdminListPage,
} from "./ui/admin-list-base.js";

@customElement("admin-page")
export class AdminPage extends AdminListBase {
  protected get listPath(): string {
    return "/admin";
  }
  protected get createHref(): string {
    return "/admin/blog/new";
  }
  protected get editHrefPrefix(): string {
    return "/admin/blog/";
  }
  protected get createLabel(): string {
    return el.admin.createPost;
  }
  protected get emptyLabel(): string {
    return el.admin.listEmpty;
  }
  protected get tab(): "blog" {
    return "blog";
  }

  protected async loadPage(
    after: string | null,
    limit: number,
    filter: AdminListFilter,
    signal: AbortSignal,
  ): Promise<AdminListPage> {
    const result = await listStaffBlogPosts(
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

/** The blog row carries the card fields the staff list already sends:
 * thumbnail, description, and the short formatted date — the favorites/shape
 * parity the owner asked for. */
function toRow(item: StaffBlogPostSummary) {
  return {
    id: item.id,
    title: item.title,
    subtitle: item.subtitle,
    status: item.status,
    updatedAt: item.updatedAt,
    thumbnailUrl: item.thumbnailUrl,
    description: item.description,
  };
}

declare global {
  interface HTMLElementTagNameMap {
    "admin-page": AdminPage;
  }
}
