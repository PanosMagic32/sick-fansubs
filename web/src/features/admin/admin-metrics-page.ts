/**
 * Admin dashboard metrics tab — the live
 * community totals and the last-30-days activity at /admin/metrics.
 *
 * Not an AdminListBase subclass: this surface paginates nothing and creates
 * nothing, so that base's paging controller, create button, and row markup
 * would all be dead weight. It extends AdminStaffPageBase directly for the
 * shared guard, first-load latch, and load-error mapping, and shares the tab
 * chrome + section styles with the content tabs.
 *
 * Every number comes straight from the server payload — the page never
 * derives a total from a list it happens to have loaded, and it never caches
 * one (the values are live aggregates).
 */

import { html, nothing, unsafeCSS } from "lit";
import type { TemplateResult } from "lit";
import { customElement, state } from "lit/decorators.js";

import { requestSignal } from "@shared/api/request-signal.js";
import { el } from "@shared/catalog/el.js";
import { formatDate } from "@shared/utils/format.js";
import { roleLabel } from "@shared/utils/roles.js";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import "@shared/ui/loading-spinner.js";
import "@shared/ui/error-banner.js";
import "./ui/admin-tabs.js";
import { AdminStaffPageBase } from "./ui/admin-staff-page-base.js";
import adminPageStyles from "./ui/admin-page.css?inline";
import styles from "./admin-metrics-page.css?inline";

import {
  getStaffMetrics,
  type StaffMetrics,
  type StaffMetricsContentTotals,
} from "./data-access/metrics-api.js";

/** One bar row: the label, the value, and the group's denominator.
 *
 * `basis` says what the denominator MEANS, and the UI reads it: a `share`
 * denominator is a real total, so the row ALSO prints its percentage; a
 * `relative` denominator is merely the group's largest counter, where a
 * percentage would claim a whole that does not exist — those groups carry a
 * caption and print counts only. */
interface BarRow {
  label: string;
  value: number;
  max: number;
  basis: "share" | "relative";
}

/** The row's fill width as a CSS percentage string; a zero denominator
renders an empty bar (never NaN). */
function percentOf(row: BarRow): string {
  if (row.max <= 0) return "0%";
  return `${Math.round((row.value / row.max) * 100)}%`;
}

/** The row's rounded share of a REAL total, or null when the denominator is
 * only the group's largest counter — the value column prints a percentage
 * for the former and nothing for the latter. */
function shareOf(row: BarRow): number | null {
  if (row.basis !== "share" || row.max <= 0) return null;
  return Math.round((row.value / row.max) * 100);
}

/** The Greek label for a KNOWN role identifier (roleLabel's string overload
 * returns the label outright). */
function roleText(role: string): string {
  return roleLabel(role);
}

@customElement("admin-metrics-page")
export class AdminMetricsPage extends AdminStaffPageBase {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(adminPageStyles),
    unsafeCSS(styles),
  ];

  @state() private metrics: StaffMetrics | null = null;

  /** The first authenticated load (the base's latch calls this once). */
  protected _start() {
    void this._load();
  }

  private async _load() {
    const controller = this._beginLoad();

    try {
      const result = await getStaffMetrics({
        signal: requestSignal(controller),
      });
      if (controller.signal.aborted) return;
      this.metrics = result;
      this.status = "success";
    } catch (err) {
      this._failLoad(err, controller);
    }
  }

  /* ── Rendering helpers ──────────────────────────────────────────── */

  private _card(label: TemplateResult | string, value: number) {
    return html`
      <div class="metrics-card">
        <span class="metrics-card-value">${value}</span>
        <span class="metrics-card-label">${label}</span>
      </div>
    `;
  }

  private _barGroup(title: TemplateResult | string | null, rows: BarRow[]) {
    return html`
      <div class="metrics-bars">
        ${title === null ? nothing : html`<h3 class="metrics-bars-title">${title}</h3>`}
        ${rows.map((row) => {
          // One read of the share: the label and the visibility test below
          // must agree, and the helper walks the row's basis.
          const share = shareOf(row);
          return html`
            <div class="metrics-bar-row">
              <span class="metrics-bar-label">${row.label}</span>
              <span class="metrics-bar-track">
                <span
                  class="metrics-bar-fill"
                  style="width: ${percentOf(row)}"
                ></span>
              </span>
              <span class="metrics-bar-value"
                >${row.value}${
                  share === null
                    ? nothing
                    : html`<span class="metrics-bar-share">${share}%</span>`
                }</span
              >
            </div>
          `;
        })}
      </div>
    `;
  }

  /** The content status bar group (published/draft/archived) — the same
   * three status labels the content tabs use. */
  private _contentBars(
    title: TemplateResult | string,
    totals: StaffMetricsContentTotals,
  ) {
    return this._barGroup(title, [
      {
        label: el.admin.statusPublished,
        value: totals.published,
        max: totals.total,
        basis: "share",
      },
      {
        label: el.admin.statusDraft,
        value: totals.draft,
        max: totals.total,
        basis: "share",
      },
      {
        label: el.admin.statusArchived,
        value: totals.archived,
        max: totals.total,
        basis: "share",
      },
    ]);
  }

  private _renderTotals(m: StaffMetrics) {
    const t = m.totals;
    const usersTotal = t.users.total;
    return html`
      <section class="metrics-section" aria-labelledby="metrics-totals-title">
        <h2 id="metrics-totals-title">${el.admin.metricsTotalsTitle}</h2>
        <p class="metrics-scale">${el.admin.metricsTotalsScale}</p>
        <div class="metrics-cards">
          ${this._card(el.admin.metricsUsers, usersTotal)}
          ${this._card(el.admin.metricsBlogPosts, t.blogPosts.total)}
          ${this._card(
            html`<span lang="en">${el.admin.metricsProjects}</span>`,
            t.projects.total,
          )}
          ${this._card(el.admin.metricsComments, t.comments)}
          ${this._card(el.admin.metricsFavorites, t.favorites)}
        </div>
        <div class="metrics-breakdowns">
          ${this._barGroup(el.admin.metricsUsersByRole, [
            {
              label: roleText("user"),
              value: t.users.byRole.user,
              max: usersTotal,
              basis: "share",
            },
            {
              label: roleText("moderator"),
              value: t.users.byRole.moderator,
              max: usersTotal,
              basis: "share",
            },
            {
              label: roleText("admin"),
              value: t.users.byRole.admin,
              max: usersTotal,
              basis: "share",
            },
            {
              label: roleText("super-admin"),
              value: t.users.byRole.superAdmin,
              max: usersTotal,
              basis: "share",
            },
          ])}
          ${this._barGroup(el.admin.metricsUsersByStatus, [
            {
              label: el.admin.statusActive,
              value: t.users.byStatus.active,
              max: usersTotal,
              basis: "share",
            },
            {
              label: el.admin.statusSuspended,
              value: t.users.byStatus.suspended,
              max: usersTotal,
              basis: "share",
            },
          ])}
          ${this._contentBars(el.admin.metricsBlogPosts, t.blogPosts)}
          ${this._contentBars(
            html`<span lang="en">${el.admin.metricsProjects}</span>`,
            t.projects,
          )}
        </div>
      </section>
    `;
  }

  private _renderActivity(m: StaffMetrics) {
    const a = m.activity30d;
    const rows = [
      { label: el.admin.metricsRegistrations, value: a.registrations },
      { label: el.admin.metricsActiveUsers, value: a.activeUsers },
      { label: el.admin.metricsSignInFailures, value: a.signInFailures },
      { label: el.admin.metricsContentCreated, value: a.contentCreated },
      { label: el.admin.metricsContentUpdated, value: a.contentUpdated },
      { label: el.admin.metricsContentDeleted, value: a.contentDeleted },
      { label: el.admin.metricsCommentDeletions, value: a.commentDeletions },
      { label: el.admin.metricsUsersSuspended, value: a.usersSuspended },
      { label: el.admin.metricsUsersReactivated, value: a.usersReactivated },
      { label: el.admin.metricsUsersDeleted, value: a.usersDeleted },
      { label: el.admin.metricsRoleChanges, value: a.roleChanges },
    ];
    // The bars share ONE denominator — the largest counter across the WHOLE
    // section, both groups — so their lengths are comparable to each other.
    // That is a RELATIVE scale, not a share of anything: `basis: "relative"`
    // keeps the percentage off these rows and the section carries a caption
    // instead (the totals bars show a share of their own group).
    const max = rows.reduce((acc, row) => Math.max(acc, row.value), 0);

    // Two half-width groups: one 48rem column would leave half the page
    // empty beside it, so the rows split across the section width and wrap
    // to one column on narrow screens.
    const half = Math.ceil(rows.length / 2);

    return html`
      <section class="metrics-section" aria-labelledby="metrics-activity-title">
        <h2 id="metrics-activity-title">${el.admin.metricsActivityTitle}</h2>
        <p class="metrics-since">
          ${el.admin.metricsSincePrefix} ${formatDate(a.since)}
        </p>
        <p class="metrics-scale">${el.admin.metricsActivityScale}</p>
        <div class="metrics-breakdowns activity-breakdowns">
          ${[rows.slice(0, half), rows.slice(half)].map((group) =>
            this._barGroup(
              null,
              group.map((row) => ({ ...row, max, basis: "relative" as const })),
            ),
          )}
        </div>
      </section>
    `;
  }

  render() {
    return html`
      <section class="admin-page">
        <admin-tabs .current=${"metrics"}></admin-tabs>

        <header class="admin-header">
          <h1>${el.admin.metricsTab}</h1>
        </header>

        ${
          this.status === "forbidden"
            ? html`<p class="admin-forbidden" role="alert">
                ${el.problems["/problems/forbidden"]}
              </p>`
            : ""
        }
        ${
          this.status === "error"
            ? html`<error-banner
                .message=${this.errorMessage}
                @retry=${() => this._load()}
              ></error-banner>`
            : ""
        }
        ${
          this.status === "loading"
            ? html`<loading-spinner></loading-spinner>`
            : ""
        }
        ${
          this.status === "success" && this.metrics
            ? html`${this._renderTotals(this.metrics)}
              ${this._renderActivity(this.metrics)}`
            : nothing
        }
      </section>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "admin-metrics-page": AdminMetricsPage;
  }
}
