/**
 * Shared admin content-list base — the staff
 * dashboard surface for BOTH content types. The blog list (admin-page, /admin)
 * and the projects list (admin-projects-page, /admin/projects) differ only
 * in their API call, create/edit addresses, and per-row secondary line —
 * the guards, tab chrome, list/pager markup, and the four states are one
 * implementation here.
 *
 * Two elements rather than one element with an internal tab switch: the
 * router REUSES an element instance across same-route navigations, and the
 * shared UrlCursorPagingController keys its re-sync on the page path — a
 * path-switching element would need to outrace the controller's own
 * navigate-event listener. Separate routes → separate elements → each page
 * has one fixed path, and the controller's wiring works untouched.
 *
 * Guarding (client-side visibility only — the server enforces) and the
 * first-load latch live in AdminStaffPageBase (the rule-of-three
 * extraction): anonymous → /sign-in, below moderator → forbidden state with
 * no list and no request. The list itself is §2-gated server-side: what the
 * API returns is what the viewer may see.
 */

import { html, LitElement, nothing, unsafeCSS } from "lit";
import { state } from "lit/decorators.js";

import { NAVIGATE_EVENT } from "@core/router.js";
import { requestSignal } from "@shared/api/request-signal.js";
import { el } from "@shared/catalog/el.js";
import {
  DEFAULT_PAGE_SIZE,
  PAGE_SIZES,
  UrlCursorPagingController,
} from "@shared/utils/url-cursor-paging.js";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import formStyles from "@shared/ui/forms.css?inline";
import "@shared/ui/loading-spinner.js";
import "@shared/ui/error-banner.js";
import "@shared/ui/pager-nav.js";
import "@shared/ui/content-card.js";
import { icons } from "@shared/ui/icons.js";
import "./admin-tabs.js";
import { AdminStaffPageBase } from "./admin-staff-page-base.js";
import styles from "./admin-page.css?inline";

export type AdminRowStatus = "draft" | "published" | "archived";

/** The status filter's values, in display order. The empty
 * string is the "every status" arm and is never sent on the wire. */
const CONTENT_STATUSES: AdminRowStatus[] = ["draft", "published", "archived"];

/** The wire bound on the title filter — the same 100 runes the
 * server enforces. The input's `maxlength` covers typing; this covers a
 * hand-edited URL, which is ignored rather than sent into a 422. */
const MAX_FILTER_QUERY_LEN = 100;

/** The filter a staff content list carries — what the page reads
 * from its URL and hands to the content type's list call. */
export interface AdminListFilter {
  status: AdminRowStatus | "";
  q: string;
}

/** The normalized list row the base renders. */
export interface AdminRow {
  id: string;
  title: string;
  status: AdminRowStatus;
  updatedAt: string;
  /** Optional inline subtitle beside the title (the blog rows carry one;
   * projects have no subtitle). */
  subtitle?: string;
  /** Optional muted secondary line — the projects list shows the slug. */
  secondary?: string;
  /** Optional thumbnail (the favorites card shape);
   * absent or null renders no image, never a broken src. */
  thumbnailUrl?: string | null;
  /** Optional one-line description, clamped by the sheet. */
  description?: string;
}

/** One loaded page: rows + the pager's continuation and count signals. */
export interface AdminListPage {
  items: AdminRow[];
  hasNext: boolean;
  endCursor: string | null;
  /** Filtered row count at read time — the pager's page-count numerator. */
  total: number;
}

export abstract class AdminListBase extends AdminStaffPageBase {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(formStyles),
    unsafeCSS(styles),
  ];

  /**
   * Subclass configuration — concrete GETTERS with throwing defaults.
   * They must be getters, not class fields: the base constructor builds
   * the paging controller before leaf field initializers run, while
   * prototype getters are live immediately. The throwing default turns a
   * leaf that forgets to override one into a loud construction error.
   */

  /** The list's URL path (the paging controller's sync key). */
  protected get listPath(): string {
    throw new Error("AdminListBase subclass must override listPath");
  }
  /** The create-form address for this content type. */
  protected get createHref(): string {
    throw new Error("AdminListBase subclass must override createHref");
  }
  /** The edit-form address prefix ("/admin/blog/" | "/admin/projects/"). */
  protected get editHrefPrefix(): string {
    throw new Error("AdminListBase subclass must override editHrefPrefix");
  }
  /** Create-button copy. */
  protected get createLabel(): string {
    throw new Error("AdminListBase subclass must override createLabel");
  }
  /** Empty-list copy. */
  protected get emptyLabel(): string {
    throw new Error("AdminListBase subclass must override emptyLabel");
  }
  /** Which tab this element IS (drives the tab chrome's aria-current). */
  protected get tab(): "blog" | "projects" {
    throw new Error("AdminListBase subclass must override tab");
  }
  /** Fetch one page (the data-access call for this content type). The filter
   * rides the request as well: it is part of what the page IS, not of how the
   * rows are paged. */
  protected abstract loadPage(
    after: string | null,
    limit: number,
    filter: AdminListFilter,
    signal: AbortSignal,
  ): Promise<AdminListPage>;

  @state() protected items: AdminRow[] = [];
  @state() protected hasNext = false;

  /** The loaded page's filtered row count (pageInfo.total) — the pager's
   * page-count numerator. Reset on every load start. */
  @state() protected total = 0;

  /** The URL-carried filters (the staff user list's role/status precedent). */
  @state() protected statusFilter: AdminRowStatus | "" = "";
  @state() protected queryFilter = "";

  /** The server's continuation cursor for the next page (non-reactive by
   * design — consumed by the pager click only). */
  protected _endCursor: string | null = null;

  protected readonly _paging: UrlCursorPagingController;

  constructor() {
    super();
    this._paging = new UrlCursorPagingController(this, {
      path: this.listPath,
      navigateEvent: NAVIGATE_EVENT,
      defaultLimit: DEFAULT_PAGE_SIZE,
      // The authenticated latch in updated() owns the first load — an
      // unconditional mount-time sync would double-fetch (the controller
      // fires before the session context can be read) and could fire an
      // unauthenticated request before the guard/latch settle.
      syncOnConnect: false,
      onLoad: (after, limit, scrollOnLoad) => {
        // The URL is the single source of truth: the filters are re-read on
        // every sync, so state can never drift from the address bar. A value
        // outside the vocabulary (a hand-edited link) is ignored rather than
        // sent — the server would 422 it — and so is an over-long q.
        const params = new URLSearchParams(window.location.search);
        const rawStatus = params.get("status");
        this.statusFilter = CONTENT_STATUSES.includes(
          rawStatus as AdminRowStatus,
        )
          ? (rawStatus as AdminRowStatus)
          : "";
        const q = (params.get("q") ?? "").trim();
        this.queryFilter = [...q].length > MAX_FILTER_QUERY_LEN ? "" : q;
        void this._load(after, limit, scrollOnLoad);
      },
      nextURL: (after, limit) => {
        const p = this._params();
        p.set("limit", String(limit));
        p.set("after", after);
        return `${this.listPath}?${p.toString()}`;
      },
      limitURL: (limit) => {
        const p = this._params();
        p.set("limit", String(limit));
        return `${this.listPath}?${p.toString()}`;
      },
    });
  }

  /** The filter params shared by every URL builder of this list. */
  private _params(): URLSearchParams {
    const p = new URLSearchParams();
    if (this.statusFilter) p.set("status", this.statusFilter);
    if (this.queryFilter) p.set("q", this.queryFilter);
    return p;
  }

  /** A filter change pushes a FIRST-page URL (a cursor is only meaningful
   * with the filter that produced it) and re-syncs from it. pushState, not
   * replaceState: it is a new position, and Previous returns to the prior
   * filter's page.
   *
   * The text filter is applied on submit, never per keystroke: every
   * keystroke would otherwise be a request AND a history entry. */
  private _pushFilterAndSync(status: AdminRowStatus | "", q: string) {
    const p = new URLSearchParams();
    if (status) p.set("status", status);
    if (q) p.set("q", q);
    const url = p.toString()
      ? `${this.listPath}?${p.toString()}`
      : this.listPath;
    window.history.pushState({}, "", url);
    this._paging.sync();
  }

  protected _onStatusFilterChange = (e: Event) => {
    const value = (e.target as HTMLSelectElement).value;
    const status = CONTENT_STATUSES.includes(value as AdminRowStatus)
      ? (value as AdminRowStatus)
      : "";
    if (
      status === this.statusFilter &&
      this._typedFilter() === this.queryFilter
    )
      return;
    // The row is ONE filter set: text typed but not yet submitted rides the
    // status change instead of being discarded (user input is never dropped
    // on the floor).
    this._pushFilterAndSync(status, this._typedFilter());
  };

  /** What the title field currently holds (trimmed), falling back to the
   * applied filter when the field is not rendered. */
  private _typedFilter(): string {
    const input =
      this.renderRoot?.querySelector<HTMLInputElement>('input[name="q"]');
    return (input?.value ?? this.queryFilter).trim();
  }

  protected _onSearchFilterSubmit = (e: Event) => {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const q = String(new FormData(form).get("q") ?? "").trim();
    if (q === this.queryFilter) return;
    this._pushFilterAndSync(this.statusFilter, q);
  };

  /** Applying a re-render's URL state imperatively (the users page's
   * `select[data-value]` rule — Lit commits a property binding before the
   * mapped options exist, so the FIRST option would win). The guard has no
   * empty-value exception: "every status" ("") is a real selection the user
   * must be able to return to, and the option check keeps a select that has
   * no such option from being blanked. The text input has no value binding at
   * all: a focused field is NEVER touched (a re-render must not eat typing),
   * and an unfocused one is corrected only when it is stale. */
  updated(changed: Parameters<LitElement["updated"]>[0]) {
    super.updated(changed);
    const root = this.renderRoot as ShadowRoot;
    root
      .querySelectorAll<HTMLSelectElement>("select[data-value]")
      .forEach((select) => {
        const want = select.dataset.value ?? "";
        if (
          select.value !== want &&
          [...select.options].some((option) => option.value === want)
        ) {
          select.value = want;
        }
      });
    const input = root.querySelector<HTMLInputElement>('input[name="q"]');
    if (
      input &&
      input !== root.activeElement &&
      input.value !== this.queryFilter
    ) {
      input.value = this.queryFilter;
    }
  }

  /** The first authenticated load (the base's latch calls this once). */
  protected _start() {
    this._paging.sync(false);
  }

  protected async _load(
    after: string | null,
    limit: number,
    scrollOnLoad: boolean,
  ) {
    const controller = this._beginLoad();
    this.total = 0;

    try {
      const result = await this.loadPage(
        after,
        limit,
        this._filter(),
        requestSignal(controller),
      );
      if (controller.signal.aborted) return;
      this.items = result.items;
      this.hasNext = result.hasNext;
      this._endCursor = result.endCursor;
      this.total = result.total;
      this.status = "success";
      // A page turn starts at the top; popstate-synced loads keep the
      // browser's restored scroll position.
      if (scrollOnLoad) window.scrollTo(0, 0);
    } catch (err) {
      this._failLoad(err, controller);
    }
  }

  private _filter(): AdminListFilter {
    return { status: this.statusFilter, q: this.queryFilter };
  }

  /** The status filter's label for one value ("" = the every-status arm). */
  protected _filterStatusLabel(status: AdminRowStatus | ""): string {
    return status === "" ? el.admin.filterStatusAll : this._statusLabel(status);
  }

  protected _statusLabel(status: AdminRowStatus): string {
    switch (status) {
      case "draft":
        return el.admin.statusDraft;
      case "archived":
        return el.admin.statusArchived;
      default:
        return el.admin.statusPublished;
    }
  }

  protected _onPrev() {
    this._paging.prev();
  }

  protected _onNext() {
    if (this._endCursor) {
      this._paging.next(this._endCursor);
    }
  }

  protected _onLimitChange(e: Event) {
    this._paging.setLimit((e as CustomEvent<number>).detail);
  }

  render() {
    return html`
      <section class="admin-page">
        <admin-tabs .current=${this.tab}></admin-tabs>

        <header class="admin-header">
          <h1>${el.admin.dashboardTitle}</h1>
          <a
            class="button button--secondary create-button"
            href=${this.createHref}
            >${this.createLabel}</a
          >
        </header>

        <fieldset class="admin-filters">
          <legend class="sr-only">${el.admin.filtersLabel}</legend>
          <label class="admin-filter">
            <span>${el.admin.filterStatusLabel}</span>
            <select
              name="status"
              data-value=${this.statusFilter}
              ?disabled=${this.status !== "success"}
              @change=${this._onStatusFilterChange}
            >
              <option value="">${this._filterStatusLabel("")}</option>
              ${CONTENT_STATUSES.map(
                (s) =>
                  html`<option value=${s}>
                    ${this._filterStatusLabel(s)}
                  </option>`,
              )}
            </select>
          </label>
          <form class="admin-search" @submit=${this._onSearchFilterSubmit}>
            <label class="admin-filter">
              <span>${el.admin.filterSearchLabel}</span>
              <input
                type="search"
                name="q"
                maxlength=${MAX_FILTER_QUERY_LEN}
                placeholder=${el.admin.filterSearchPlaceholder}
                ?disabled=${this.status !== "success"}
              />
            </label>
            <button
              type="submit"
              class="button button--secondary button--sm admin-filter-apply"
              ?disabled=${this.status !== "success"}
            >
              ${el.admin.filterApply}
            </button>
          </form>
        </fieldset>

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
                  @retry=${() => this._paging.sync(false)}
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
                <p class="admin-empty" role="status">
                  ${
                    this.statusFilter || this.queryFilter
                      ? el.admin.listEmptyFiltered
                      : this.emptyLabel
                  }
                </p>
              </div>`
            : ""
        }
        ${
          this.status === "success" && this.items.length > 0
            ? html`
                <ul class="admin-list" role="list">
                  ${this.items.map(
                    (item) => html`
                      <li>
                        <content-card
                          shape="row"
                          dense
                          heading-level="0"
                          title=${item.title}
                          subtitle=${item.subtitle ?? ""}
                          badge=${this._statusLabel(item.status)}
                          badge-tone=${item.status}
                          secondary=${item.secondary ?? ""}
                          description=${item.description ?? ""}
                          thumbnail-url=${item.thumbnailUrl ?? nothing}
                          meta-label=${el.blog.updated}
                          meta-instant=${item.updatedAt || ""}
                        >
                          <a
                            slot="actions"
                            class="button button--icon button--ghost edit-link"
                            href="${this.editHrefPrefix}${item.id}"
                            aria-label=${el.admin.edit}
                            title=${el.admin.edit}
                            >${icons.edit}</a
                          >
                        </content-card>
                      </li>
                    `,
                  )}
                </ul>
                <pager-nav
                  .hasPrevious=${this._paging.hasPrevious}
                  .hasNext=${this.hasNext}
                  .limit=${this._paging.limit}
                  .page=${this._paging.page}
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
