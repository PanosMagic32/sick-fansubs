/**
 * Admin users tab — the staff user list at
 * /admin/users. Filterable (role + status) with URL-carried cursor paging.
 *
 * A separate element from AdminListBase: the base renders content cards
 * with create/edit affordances and status badges; the member rows are an
 * interactive management surface (staged selects, a per-member panel), and
 * the page carries two FILTER parameters that must ride the pager URLs. The
 * member rows are the recorded exception to the content-card rule
 * (components.md rule 15). The guard, the authenticated latch, and the
 * load-error mapping come from `AdminStaffPageBase`.
 *
 * The URL is the single source of truth: role/status/limit/after all ride
 * location.search, so refresh/back/share keep working (the search-page
 * pattern). A filter change pushes a NEW first-page URL (a cursor is only
 * meaningful with the same filter); Previous (history.back) restores the
 * prior filter's page.
 *
 * email is server-authoritative: the API omits it for moderator viewers,
 * and the client renders it only when the field arrives — never guessed
 * from the viewer's role client-side.
 *
 * Member rows are collapsed cards: a tap opens ONE member's panel, where
 * the role and status are STAGED selects committed by a single save, and
 * the two immediate destructive actions (reset password, delete) are icon
 * buttons. The row markup, its staged state, and its actions live in
 * `ui/member-section.ts` — the section controller renders them into this
 * page's shadow root.
 */

import { html, LitElement, unsafeCSS } from "lit";
import { customElement, state } from "lit/decorators.js";

import { NAVIGATE_EVENT } from "@core/router.js";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import formStyles from "@shared/ui/forms.css?inline";
import { requestSignal } from "@shared/api/request-signal.js";
import { el } from "@shared/catalog/el.js";
import { roleLabel } from "@shared/utils/roles.js";
import {
  DEFAULT_PAGE_SIZE,
  PAGE_SIZES,
  UrlCursorPagingController,
} from "@shared/utils/url-cursor-paging.js";
import avatarStyles from "@shared/ui/avatar.css?inline";
import "@shared/ui/loading-spinner.js";
import "@shared/ui/error-banner.js";
import "@shared/ui/pager-nav.js";
import type { SessionState } from "@features/auth/data-access/session.js";

import "./ui/admin-tabs.js";
import { AdminStaffPageBase } from "./ui/admin-staff-page-base.js";
import { MemberSection, ROLES, STATUSES } from "./ui/member-section.js";
// The dashboard's page shell (.admin-page/.admin-header + .admin-empty);
// this page's own list chrome lives in its stylesheet below.
import adminPageStyles from "./ui/admin-page.css?inline";
import styles from "./admin-users-page.css?inline";
import {
  listStaffUsers,
  type StaffUser,
  type UserRole,
  type UserStatus,
} from "./data-access/users-admin-api.js";

const USERS_PATH = "/admin/users";

@customElement("admin-users-page")
export class AdminUsersPage extends AdminStaffPageBase {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(formStyles),
    unsafeCSS(adminPageStyles),
    unsafeCSS(avatarStyles),
    unsafeCSS(styles),
  ];

  @state() private items: StaffUser[] = [];

  @state() private hasNext = false;

  /** The loaded page's filtered row count (pageInfo.total) — the pager's
   * page-count numerator. Reset on every load start/error. */
  @state() private total = 0;
  @state() private roleFilter: UserRole | null = null;
  @state() private statusFilter: UserStatus | null = null;

  /** The member section: the member cards, the staged-edit panel, and every
   * row action (save, password reset, delete) — it renders into this shadow
   * root and reads the page through the host surface below. */
  private readonly members: MemberSection = new MemberSection(this);

  /** The server's continuation cursor for the next page (non-reactive —
   * consumed by the pager click only). */
  private _endCursor: string | null = null;

  /** The URL-carried cursor paging state machine (shared). The page keeps
   * its role/status params and the load; the controller owns the URL sync,
   * the refresh-safe hasPrevious marker, the limit, and the Next/Previous
   * mechanics. */
  private readonly paging = new UrlCursorPagingController(this, {
    path: USERS_PATH,
    navigateEvent: NAVIGATE_EVENT,
    defaultLimit: DEFAULT_PAGE_SIZE,
    // Same contract as the admin list base: the authenticated latch in
    // updated() owns the first load — a mount-time sync would double-fetch
    // and could fire before the session context exists.
    syncOnConnect: false,
    nextURL: (after, limit) => {
      const p = this._params(this.roleFilter, this.statusFilter);
      p.set("limit", String(limit));
      p.set("after", after);
      return `${USERS_PATH}?${p.toString()}`;
    },
    limitURL: (limit) => {
      const p = this._params(this.roleFilter, this.statusFilter);
      p.set("limit", String(limit));
      return `${USERS_PATH}?${p.toString()}`;
    },
    onLoad: (after, limit, scrollOnLoad) => {
      // The URL is the single source of truth: role/status are re-read
      // here on every sync, so state can never drift from the address
      // bar. Invalid values normalize to unset (the search-page's type
      // handling) so hand-edited garbage never reaches the API.
      const params = new URLSearchParams(window.location.search);
      const rawRole = params.get("role");
      const role = ROLES.includes(rawRole as UserRole)
        ? (rawRole as UserRole)
        : null;
      const rawStatus = params.get("status");
      const status = STATUSES.includes(rawStatus as UserStatus)
        ? (rawStatus as UserStatus)
        : null;
      this.roleFilter = role;
      this.statusFilter = status;
      void this._load(after, limit, scrollOnLoad);
    },
  });

  /** The first authenticated sync (the base's latch calls this once). */
  protected _start() {
    this.paging.sync(false);
  }

  updated(changed: Parameters<LitElement["updated"]>[0]) {
    super.updated(changed);
    // Re-apply every controlled select's value. An <option>'s selectedness is
    // assigned while the option is still DETACHED — Lit binds the mapped
    // children before the select joins the DOM — and a DOM implementation that
    // does not re-run the selectedness algorithm on insertion keeps the FIRST
    // option chosen (a row's role select would read «Μέλος»). Setting the
    // value once the DOM exists is deterministic everywhere, and a no-op where
    // the browser already resolved it. Every select whose options are mapped
    // in the same render carries `data-value` (the filters, the panel's role
    // and status selects), so one pass covers the whole page.
    this.shadowRoot
      ?.querySelectorAll<HTMLSelectElement>("select[data-value]")
      .forEach((select) => {
        const want = select.dataset.value ?? "";
        if (want !== "" && select.value !== want) select.value = want;
      });
    // The one-time password surface is a native modal <dialog>: opening it
    // has no attribute form, so it is a post-render step (idempotent via
    // the dialog.open check).
    this._showResetDialog();
  }

  /* ── The member section's host surface (MemberSectionHost) ──────── */

  /** The page's session state — the section reads the actor role from it. */
  get sessionState(): SessionState | undefined {
    return this.session?.state;
  }

  /** Replace one loaded row with the server's projection (the section's
   * save/status-change row effect). */
  replaceMember(updated: StaffUser) {
    this.items = this.items.map((it) => (it.id === updated.id ? updated : it));
  }

  /** Remove one deleted row. The page lost its last row: step back to the
   * previous page when one exists (URL-carried paging), else re-sync — a
   * genuinely empty page renders the empty state. Partial pages update in
   * place (no flash). */
  removeMember(id: string) {
    const rest = this.items.filter((it) => it.id !== id);
    this.items = rest;
    if (rest.length === 0) {
      if (this.paging.hasPrevious) {
        this.paging.prev();
      } else {
        this.paging.sync(false);
      }
    }
  }

  /** Open the rendered reset dialog as a modal: showModal() supplies the
   * focus trap, focus restore, Escape handling, and the inert backdrop; the
   * guarded open-attribute fallback is the recorded dialog shape
   * (docs/patterns/web/components.md). */
  private _showResetDialog() {
    const dialog = this.shadowRoot?.querySelector<HTMLDialogElement>(
      "dialog.reset-dialog",
    );
    if (!dialog || dialog.open) return;
    if (typeof dialog.showModal === "function") {
      dialog.showModal();
    } else {
      dialog.setAttribute("open", "");
    }
  }

  /* ── URL sync ───────────────────────────────────────────────────── */

  /** role/status params shared by every users URL builder. */
  private _params(
    role: UserRole | null,
    status: UserStatus | null,
  ): URLSearchParams {
    const p = new URLSearchParams();
    if (role) p.set("role", role);
    if (status) p.set("status", status);
    return p;
  }

  /** Filter change: push the new FIRST-page URL (a cursor is only
   * meaningful with the same filter), then re-sync from it. pushState
   * (not replaceState) — a filter change is a new position, and Previous
   * restores the prior filter's page. */
  private _pushFilterAndSync(role: UserRole | null, status: UserStatus | null) {
    const p = this._params(role, status);
    const url = p.toString() ? `${USERS_PATH}?${p.toString()}` : USERS_PATH;
    window.history.pushState({}, "", url);
    this.paging.sync();
  }

  private _onRoleChange = (e: Event) => {
    const value = (e.target as HTMLSelectElement).value;
    this._pushFilterAndSync(
      value === "" ? null : (value as UserRole),
      this.statusFilter,
    );
  };

  private _onStatusChange = (e: Event) => {
    const value = (e.target as HTMLSelectElement).value;
    this._pushFilterAndSync(
      this.roleFilter,
      value === "" ? null : (value as UserStatus),
    );
  };

  /* ── Data ───────────────────────────────────────────────────────── */

  private async _load(
    after: string | null,
    limit: number,
    scrollOnLoad: boolean,
  ) {
    const controller = this._beginLoad();
    this.total = 0;
    this.members.resetForLoad();

    try {
      const page = await listStaffUsers(
        {
          limit,
          ...(after ? { after } : {}),
          ...(this.roleFilter ? { role: this.roleFilter } : {}),
          ...(this.statusFilter ? { status: this.statusFilter } : {}),
        },
        { signal: requestSignal(controller) },
      );
      if (controller.signal.aborted) return;
      this.items = page.items;
      this.hasNext = page.pageInfo.hasNextPage;
      this._endCursor = page.pageInfo.endCursor;
      this.total = page.pageInfo.total;
      this.status = "success";
      // A page turn starts at the top; popstate-synced loads keep the
      // browser's restored scroll position.
      if (scrollOnLoad) window.scrollTo(0, 0);
    } catch (err) {
      this._failLoad(err, controller);
    }
  }

  /* ── Paging ───────────────────────────────────────────────────── */

  private _onPrev = () => {
    this.paging.prev();
  };

  private _onNext = () => {
    if (this._endCursor) {
      this.paging.next(this._endCursor);
    }
  };

  private _onLimitChange = (e: Event) => {
    this.paging.setLimit((e as CustomEvent<number>).detail);
  };

  render() {
    return html`
      <section class="admin-page">
        <admin-tabs current="users"></admin-tabs>

        <header class="admin-header">
          <h1>${el.admin.usersTitle}</h1>
        </header>

        <div
          class="users-filters"
          role="group"
          aria-label=${el.admin.usersFiltersLabel}
        >
          <label class="users-filter">
            <span>${el.admin.filterRoleLabel}</span>
            <select
              .value=${this.roleFilter ?? ""}
              data-value=${this.roleFilter ?? ""}
              @change=${this._onRoleChange}
              ?disabled=${this.status !== "success"}
            >
              <option value="">${el.admin.filterRoleAll}</option>
              ${ROLES.map(
                (r) => html`<option value=${r}>${roleLabel(r)}</option>`,
              )}
            </select>
          </label>
          <label class="users-filter">
            <span>${el.admin.filterStatusLabel}</span>
            <select
              .value=${this.statusFilter ?? ""}
              data-value=${this.statusFilter ?? ""}
              @change=${this._onStatusChange}
              ?disabled=${this.status !== "success"}
            >
              <option value="">${el.admin.filterStatusAll}</option>
              ${STATUSES.map(
                (s) =>
                  html`<option value=${s}>
                    ${this.members.statusLabel(s)}
                  </option>`,
              )}
            </select>
          </label>
        </div>

        ${
          this.members.resetError
            ? html`<error-banner
                .message=${this.members.resetError}
              ></error-banner>`
            : ""
        }
        ${
          this.members.opError
            ? html`<error-banner
                .message=${this.members.opError}
              ></error-banner>`
            : ""
        }
        ${
          this.members.resetResult
            ? html`
                <dialog
                  class="reset-dialog"
                  aria-labelledby="reset-dialog-title"
                  @cancel=${this.members.onCloseReset}
                >
                  <h2 id="reset-dialog-title" class="reset-dialog-title">
                    ${el.admin.resetResultTitle}
                  </h2>
                  <p>${el.admin.resetResultIntro}</p>
                  <div class="reset-password-row">
                    <span class="sr-only">${el.admin.resetPasswordLabel}</span>
                    <code>${this.members.resetResult.password}</code>
                    <span class="reset-dialog-buttons">
                      <button
                        type="button"
                        class="button button--secondary button--sm reset-copy"
                        @click=${this.members.onCopyReset}
                      >
                        ${
                          this.members.copied
                            ? el.admin.resetCopied
                            : el.admin.resetCopy
                        }
                      </button>
                      <button
                        type="button"
                        class="button button--secondary button--sm reset-close"
                        @click=${this.members.onCloseReset}
                      >
                        ${el.admin.resetClose}
                      </button>
                    </span>
                  </div>
                </dialog>
              `
            : ""
        }
        ${
          this.status === "forbidden"
            ? html`<p class="admin-forbidden" role="alert">
                ${el.problems["/problems/forbidden"]}
              </p>`
            : ""
        }
        ${
          this.status === "error"
            ? html`<div class="state-block">
                <error-banner
                  .message=${this.errorMessage}
                  @retry=${() => this.paging.sync(false)}
                ></error-banner>
              </div>`
            : ""
        }
        ${
          this.status === "loading"
            ? html`<div class="state-block">
                <loading-spinner></loading-spinner>
              </div>`
            : ""
        }
        ${
          this.status === "success" && this.items.length === 0
            ? html`<div class="state-block">
                <p class="admin-empty" role="status">${el.admin.usersEmpty}</p>
              </div>`
            : ""
        }
        ${
          this.status === "success" && this.items.length > 0
            ? html`
                <ul class="members-list" role="list">
                  ${this.items.map((item) => this.members.template(item))}
                </ul>
                <pager-nav
                  .hasPrevious=${this.paging.hasPrevious}
                  .hasNext=${this.hasNext}
                  .limit=${this.paging.limit}
                  .page=${this.paging.page}
                  .total=${this.total}
                  .limits=${PAGE_SIZES}
                  @sf-pager-prev=${this._onPrev}
                  @sf-pager-next=${this._onNext}
                  @sf-limit-change=${this._onLimitChange}
                ></pager-nav>
              `
            : ""
        }
      </section>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "admin-users-page": AdminUsersPage;
  }
}
