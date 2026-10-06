/**
 * Admin dashboard tab chrome — the blog/projects/users/metrics tab row,
 * plus the super-admin-only logs tab.
 *
 * One element keeps a single copy of the markup and the current-state logic
 * instead of one per surface (the rule-of-three graduation). The current
 * tab drives aria-current; navigation is plain links — the router handles
 * them.
 *
 * The logs tab renders only for a super-admin, so a moderator never sees a
 * link that would answer 403. That is why this element reads the session
 * context — a flag passed by each page would hide the tab on every page
 * but the logs one.
 */

import { consume } from "@lit/context";
import { html, LitElement, nothing, unsafeCSS } from "lit";
import { customElement, property } from "lit/decorators.js";

import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import {
  sessionContext,
  type SessionContext,
} from "@features/auth/data-access/session.js";
import { el } from "@shared/catalog/el.js";
import { isSuperAdminRole } from "@shared/utils/roles.js";
import styles from "./admin-tabs.css?inline";

export type AdminTab = "blog" | "projects" | "users" | "metrics" | "logs";

@customElement("admin-tabs")
export class AdminTabs extends LitElement {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(styles),
  ];

  /** Which tab this surface IS (drives the aria-current marker). */
  @property() current: AdminTab = "blog";

  /** The session, read only to decide whether the super-admin tab exists. */
  @consume({ context: sessionContext, subscribe: true })
  private session?: SessionContext;

  render() {
    return html`
      <nav class="admin-tabs" aria-label=${el.admin.tabsLabel}>
        <a
          class="admin-tab"
          href="/admin"
          aria-current=${this.current === "blog" ? "page" : nothing}
          >${el.admin.contentTab}</a
        >
        <a
          class="admin-tab"
          href="/admin/projects"
          lang="en"
          aria-current=${this.current === "projects" ? "page" : nothing}
          >${el.admin.projectsTab}</a
        >
        <a
          class="admin-tab"
          href="/admin/users"
          aria-current=${this.current === "users" ? "page" : nothing}
          >${el.admin.usersTab}</a
        >
        <a
          class="admin-tab"
          href="/admin/metrics"
          aria-current=${this.current === "metrics" ? "page" : nothing}
          >${el.admin.metricsTab}</a
        >
        ${
          this._showLogsTab()
            ? html`<a
                class="admin-tab"
                href="/admin/logs"
                aria-current=${this.current === "logs" ? "page" : nothing}
                >${el.admin.logsTab}</a
              >`
            : nothing
        }
      </nav>
    `;
  }

  /** The logs tab is super-admin only. Without a session the link
   * stays hidden: an unauthenticated visitor is being redirected anyway, and
   * showing a tab they cannot open would be the wrong hint. */
  private _showLogsTab(): boolean {
    const s = this.session?.state;
    return s?.status === "authenticated" && isSuperAdminRole(s.user.role);
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "admin-tabs": AdminTabs;
  }
}
