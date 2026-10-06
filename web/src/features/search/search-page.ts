/**
 * Search page — the public merged search UI.
 *
 * Behavior:
 *  - Submit-driven (live-as-you-type is deliberately out of scope):
 *    the form submits to /search?q=…&type=…&sort=…&from=…&to=… and the URL
 *    always carries the current query, refinements, AND page cursor (after),
 *    so refresh/back/share keep working.
 *  - The type segmented control, the sort select, and the two date bounds
 *    write the URL immediately — even before a query exists, so a
 *    refinement is never discarded. Only a non-empty q triggers a request:
 *    with no query the page stays on its idle prompt and just keeps the
 *    picked refinements in the address bar.
 *  - This page's form owns the only search input.
 *
 * States are deliberate: idle (no query yet — the prompt),
 * loading, error + retry, empty, and success with URL-carried cursor
 * paging (Next pushes the continuation cursor; Previous is
 * history.back()). Pages REPLACE results — there is no load-more.
 *
 * The URL-sync/paging state machine lives in the shared
 * UrlCursorPagingController (shared/utils/url-cursor-paging.ts); the page
 * owns its query/type/sort/window params and the load itself.
 *
 * The merged list is single-column under the shell's default 48rem cap;
 * each item is the shared content-card ROW shape (roomy geometry, with the
 * visible type badge as its badge input) and links to its detail route.
 * thumbnailUrl is null-guarded — the API emits null for masked values,
 * never "".
 */

import { html, LitElement, nothing, unsafeCSS } from "lit";
import { customElement, state } from "lit/decorators.js";

import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import formStyles from "@shared/ui/forms.css?inline";
import { NAVIGATE_EVENT } from "@core/router.js";
import { mapError } from "@shared/utils/format.js";
import {
  DEFAULT_PAGE_SIZE,
  PAGE_SIZES,
  UrlCursorPagingController,
} from "@shared/utils/url-cursor-paging.js";
import { el } from "@shared/catalog/el.js";
import "@shared/ui/loading-spinner.js";
import "@shared/ui/error-banner.js";
import "@shared/ui/content-card.js";

import { searchContent } from "./data-access/search-api.js";
import type {
  SearchResultItem,
  SearchSort,
  SearchType,
} from "./data-access/types.js";
import styles from "./search-page.css?inline";

import { requestSignal } from "@shared/api/request-signal.js";
import "@shared/ui/pager-nav.js";

type PageStatus = "idle" | "loading" | "error" | "success";

const SEARCH_PATH = "/search";

/** Accepted type values, in display order. */
const TYPES: SearchType[] = ["all", "posts", "projects"];

/** Accepted sort values, in display order. */
const SORTS: SearchSort[] = ["date", "oldest", "title"];

/** A canonical calendar day — the only shape the wire contract accepts for
 * the from/to window (the server rejects anything else with a 422). */
const DATE_PATTERN = /^\d{4}-\d{2}-\d{2}$/;

/** The wire bound on `q` (`searchContent`'s `q` maxLength, in runes) —
 * the server answers 422 above it. A longer URL-supplied value is ignored
 * rather than sent, because no retry could succeed. */
const MAX_QUERY_RUNES = 100;

/** Read one URL-supplied date bound.
 *
 * The value must be a canonical calendar day inside the range the server
 * accepts (1970-9999): the URL is user-editable, and a bound the server
 * would reject must never be echoed back into a request — that would leave
 * the page in a 422 error state for a link someone merely bookmarked. An
 * inverted window (from > to) is NOT corrected here: silently reordering the
 * user's bounds would hide the mistake, so the server's 422 owns that case. */
function readDateBound(raw: string | null): string {
  if (raw === null || !DATE_PATTERN.test(raw)) return "";
  const parts = raw.split("-");
  const year = Number(parts[0]);
  const month = Number(parts[1]);
  const day = Number(parts[2]);
  if (year < 1970) return "";
  const date = new Date(Date.UTC(year, month - 1, day));
  // A day the calendar rejects (2025-02-30) rolls over into the next month.
  if (
    date.getUTCFullYear() !== year ||
    date.getUTCMonth() !== month - 1 ||
    date.getUTCDate() !== day
  ) {
    return "";
  }
  return raw;
}

@customElement("search-page")
export class SearchPage extends LitElement {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(formStyles),
    unsafeCSS(styles),
  ];

  @state() private status: PageStatus = "idle";
  @state() private q = "";
  @state() private draftEmpty = true;
  @state() private type: SearchType = "all";
  @state() private sort: SearchSort = "date";
  @state() private from = "";
  @state() private to = "";
  @state() private items: SearchResultItem[] = [];
  @state() private hasNextPage = false;
  @state() private endCursor: string | null = null;
  /** The loaded page's filtered row count (pageInfo.total) — the pager's
   * page-count numerator. Reset on every load start/error. */
  @state() private total = 0;
  @state() private errorMessage = "";

  /** In-flight request controller — aborted on teardown or newer loads. */
  private _abort: AbortController | null = null;

  /** The URL-carried cursor paging state machine (shared). The page
   * keeps its query/type params and the load; the controller owns the
   * URL sync, the refresh-safe hasPrevious marker, the limit, and the
   * Next/Previous mechanics (see shared/utils/url-cursor-paging.ts). */
  private readonly paging = new UrlCursorPagingController(this, {
    path: SEARCH_PATH,
    navigateEvent: NAVIGATE_EVENT,
    defaultLimit: DEFAULT_PAGE_SIZE,
    nextURL: (after, limit) => {
      const p = this._params(this.q, this.type, this.sort, this.from, this.to);
      p.set("limit", String(limit));
      p.set("after", after);
      return `${SEARCH_PATH}?${p.toString()}`;
    },
    limitURL: (limit) => {
      const p = this._params(this.q, this.type, this.sort, this.from, this.to);
      p.set("limit", String(limit));
      return `${SEARCH_PATH}?${p.toString()}`;
    },
    onLoad: (after, limit, scrollOnLoad) => {
      // The URL is the single source of truth: q/type/sort/from/to are
      // re-read here on every sync, so state can never drift from the
      // address bar.
      const params = new URLSearchParams(window.location.search);
      const rawQ = (params.get("q") ?? "").trim();
      // A URL-supplied q beyond the wire bound is ignored: the server would
      // 422 it and a retry cannot succeed.
      const q = [...rawQ].length > MAX_QUERY_RUNES ? "" : rawQ;
      const rawType = params.get("type");
      const type: SearchType =
        rawType === "posts" || rawType === "projects" ? rawType : "all";
      const rawSort = params.get("sort");
      const sort: SearchSort =
        rawSort === "title" || rawSort === "oldest" ? rawSort : "date";
      const previousQ = this.q;
      this.q = q;
      if (q !== previousQ) this.draftEmpty = q === "";
      this.type = type;
      this.sort = sort;
      this.from = readDateBound(params.get("from"));
      this.to = readDateBound(params.get("to"));
      if (q) {
        void this._load(after, limit, scrollOnLoad);
      } else {
        this._abort?.abort();
        this.status = "idle";
        this.items = [];
        this.total = 0;
      }
    },
  });

  /** happy-dom ignores the `selected` attribute on options (browsers honor
   * it), so the active sort is applied to the select imperatively after every
   * render — otherwise the first option wins (the pager-nav size select's
   * recorded gotcha). The two date inputs are applied the same way:
   * Lit commits a part only when its value CHANGED, so a plain binding would
   * still write a state value over a field the user is mid-edit in — the
   * imperative pass has no such window, because it skips a FOCUSED date input
   * entirely and only corrects an unfocused one that is stale. */
  protected override updated() {
    const select = this.renderRoot.querySelector(
      'select[name="sort"]',
    ) as HTMLSelectElement | null;
    if (select && select.value !== this.sort) select.value = this.sort;

    const root = this.renderRoot as ShadowRoot;
    for (const input of root.querySelectorAll<HTMLInputElement>(
      'input[type="date"]',
    )) {
      const want = input.name === "from" ? this.from : this.to;
      if (input !== root.activeElement && input.value !== want) {
        input.value = want;
      }
    }
  }

  disconnectedCallback() {
    this._abort?.abort();
    super.disconnectedCallback();
  }

  /* ── URL sync ───────────────────────────────────────────────────── */

  /** q/type/sort/window params shared by every search URL builder. An empty
   * query is OMITTED rather than sent as `q=`: the refinement
   * controls write the URL before a search runs, and `/search?from=…` is the
   * honest address for "a date is picked, no query yet" — the page stays on
   * its idle prompt and fetches nothing. */
  private _params(
    q: string,
    type: SearchType,
    sort: SearchSort,
    from: string,
    to: string,
  ): URLSearchParams {
    const p = new URLSearchParams();
    if (q) p.set("q", q);
    if (type !== "all") p.set("type", type);
    if (sort !== "date") p.set("sort", sort);
    if (from) p.set("from", from);
    if (to) p.set("to", to);
    return p;
  }

  /** Write q/type/sort/window into the URL without a full navigation — the
   * controller's sync then re-reads them. Deliberately drops after/limit/page:
   * a changed query, scope, ordering, or window starts at the first page (a
   * cursor is only meaningful with the q+type+sort+window that produced it).
   * A wholly-default refinement set produces the bare path, not a dangling
   * `?`. */
  private _buildURL(
    q: string,
    type: SearchType,
    sort: SearchSort,
    from: string,
    to: string,
  ): string {
    const params = this._params(q, type, sort, from, to).toString();
    return params ? `${SEARCH_PATH}?${params}` : SEARCH_PATH;
  }

  /** pushState the URL, then re-sync from it (the URL is the single
   * source of truth — every submit/type-change/clear flows through here). */
  private _pushAndSync(url: string) {
    window.history.pushState({}, "", url);
    this.paging.sync();
  }

  /* ── Handlers ───────────────────────────────────────────────────── */

  private _onSubmit = (e: Event) => {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const data = new FormData(form);
    const q = String(data.get("q") ?? "").trim();
    if (!q) return; // stay on the idle state
    this._pushAndSync(
      this._buildURL(q, this.type, this.sort, this.from, this.to),
    );
  };

  /** Track the field's live emptiness so the clear (×) control can appear for
   * a typed draft that is not submitted yet; the URL state (q) stays
   * submit-driven. */
  private _onQueryInput = (e: Event) => {
    const empty = (e.target as HTMLInputElement).value === "";
    if (empty !== this.draftEmpty) this.draftEmpty = empty;
  };

  private _onTypeChange = (e: Event) => {
    const input = e.target as HTMLInputElement;
    const type = input.value as SearchType;
    if (type === this.type) return;
    this._pushAndSync(
      this._buildURL(this.q, type, this.sort, this.from, this.to),
    );
  };

  private _onSortChange = (e: Event) => {
    const select = e.target as HTMLSelectElement;
    const sort: SearchSort =
      select.value === "title" || select.value === "oldest"
        ? select.value
        : "date";
    if (sort === this.sort) return;
    this._pushAndSync(
      this._buildURL(this.q, this.type, sort, this.from, this.to),
    );
  };

  /** Either publication-date bound changed.
   *
   * Both `input` and `change` are wired: browsers differ on which
   * of the pair a date PICKER fires, and the picker's own commit can arrive
   * after our handler has already read the field — listening to both makes the
   * control order-independent. The no-op guard below is what keeps the pair
   * from pushing (and fetching) twice: the second event carries the value the
   * first one already applied.
   *
   * A refinement is kept even while the page is idle — it rides the URL like
   * every other control, so a picked date is never silently discarded. No
   * query means no request; the value is waiting when the user submits one. */
  private _onDateChange = (e: Event) => {
    const input = e.target as HTMLInputElement;
    const value = readDateBound(input.value);
    // A pick the server would reject must not ride the URL: a bound is a
    // value in the address bar someone can bookmark into a 422. Clear the
    // field with it, so the URL, the request, and the control always agree.
    if (value === "" && input.value !== "") input.value = "";
    const from = input.name === "from" ? value : this.from;
    const to = input.name === "to" ? value : this.to;
    if (from === this.from && to === this.to) return;
    this._pushAndSync(this._buildURL(this.q, this.type, this.sort, from, to));
  };

  /* ── Data ───────────────────────────────────────────────────────── */

  private async _load(
    after: string | null,
    limit: number,
    scrollOnLoad: boolean,
  ) {
    this._abort?.abort();
    const controller = new AbortController();
    this._abort = controller;
    this.status = "loading";
    this.total = 0;

    try {
      const page = await searchContent(
        {
          q: this.q,
          type: this.type,
          sort: this.sort,
          ...(this.from ? { from: this.from } : {}),
          ...(this.to ? { to: this.to } : {}),
          limit,
          ...(after ? { after } : {}),
        },
        { signal: requestSignal(controller) },
      );
      if (controller.signal.aborted) return; // superseded by a newer load
      this.items = page.items;
      this.hasNextPage = page.pageInfo.hasNextPage;
      this.endCursor = page.pageInfo.endCursor;
      this.total = page.pageInfo.total;
      this.status = "success";
      // A page turn starts at the top; popstate-synced loads keep the
      // browser's restored scroll position.
      if (scrollOnLoad) window.scrollTo(0, 0);
    } catch (err) {
      if (controller.signal.aborted) return;
      this.errorMessage = mapError(err);
      this.total = 0;
      this.status = "error";
    }
  }

  /* ── Paging (URL-carried cursor) ────────────────────────────────── */

  /** Next page: the shared controller pushes the CURRENT page's
   * continuation cursor into the URL and re-syncs. */
  private _onNextClick = () => {
    if (!this.endCursor) return;
    this.paging.next(this.endCursor);
  };

  /** Previous page: history.back() via the controller — popstate re-syncs
   * from the restored URL. */
  private _onPrevClick = () => {
    this.paging.prev();
  };

  /** Page-size change: the controller replaceState's the first page with
   * the new size and re-syncs (a cursor is only meaningful with the same
   * limit). */
  private _onLimitChange = (e: Event) => {
    this.paging.setLimit((e as CustomEvent<number>).detail);
  };

  /** Retry after a failure: re-sync from the URL (which still carries the
   * same query + cursor — the failed page turn is retried as-is). */
  private _onRetry = () => {
    this.paging.sync();
  };

  /** Clear the query: back to the bare /search URL and the idle state. The
   * field itself is cleared too (it may hold an unsubmitted draft), and focus
   * returns to the query input after the re-render removes the button that
   * held it. */
  private _onClearClick = async () => {
    const input =
      this.renderRoot.querySelector<HTMLInputElement>('input[name="q"]');
    if (input) input.value = "";
    this.draftEmpty = true;
    this._pushAndSync(SEARCH_PATH);
    await this.updateComplete;
    this.renderRoot.querySelector<HTMLInputElement>('input[name="q"]')?.focus();
  };

  /* ── Render ─────────────────────────────────────────────────────── */

  /** Badge copy for a RESULT item — the API discriminator is the singular
   * "post" | "project" (the filter values are the plural "posts" |
   * "projects"; the two sets must not be confused). */
  private _badgeLabel(type: "post" | "project") {
    return type === "project"
      ? html`<span lang="en">${el.search.typeProjects}</span>`
      : el.search.typePosts;
  }

  /** Label copy for one TYPE FILTER option (all | posts | projects). */
  private _filterLabel(type: SearchType) {
    if (type === "projects") {
      // Anglicism — render inside lang="en" (same policy as the nav).
      return html`<span lang="en">${el.search.typeProjects}</span>`;
    }
    return type === "posts" ? el.search.typePosts : el.search.typeAll;
  }

  /** Label copy for one SORT option (date | oldest | title). */
  private _sortLabel(sort: SearchSort) {
    if (sort === "title") return el.search.sortTitle;
    return sort === "oldest" ? el.search.sortOldest : el.search.sortDate;
  }

  render() {
    return html`
      <h1>${el.titles.search}</h1>

      <form class="search-form" role="search" @submit=${this._onSubmit}>
        <div class="search-row">
          <div class="search-input-wrap">
            <input
              type="search"
              name="q"
              .value=${this.q}
              placeholder=${el.search.placeholder}
              aria-label=${el.nav.search}
              maxlength="100"
              @input=${this._onQueryInput}
            />
            ${
              this.q || !this.draftEmpty
                ? html`
                    <button
                      class="button button--icon button--ghost clear-btn"
                      type="button"
                      aria-label=${el.search.clear}
                      @click=${this._onClearClick}
                    >
                      ×
                    </button>
                  `
                : ""
            }
          </div>
          <button type="submit" class="button button--primary submit">
            ${el.search.submit}
          </button>
        </div>

        <div class="filter-row">
          <fieldset class="type-filter">
            <legend class="sr-only">${el.search.typeLabel}</legend>
            ${TYPES.map(
              (t) => html`
                <label class="type-option">
                  <input
                    type="radio"
                    name="type"
                    value=${t}
                    ?checked=${this.type === t}
                    @change=${this._onTypeChange}
                  />
                  <span>${this._filterLabel(t)}</span>
                </label>
              `,
            )}
          </fieldset>

          <label class="sort-control">
            <span class="sort-label">${el.search.sortLabel}</span>
            <select name="sort" @change=${this._onSortChange}>
              ${SORTS.map(
                (s) => html`<option value=${s}>${this._sortLabel(s)}</option>`,
              )}
            </select>
          </label>
        </div>

        <fieldset class="date-row">
          <legend class="sr-only">${el.search.dateLabel}</legend>
          <label class="date-control">
            <span>${el.search.dateFrom}</span>
            <input
              type="date"
              name="from"
              @input=${this._onDateChange}
              @change=${this._onDateChange}
            />
          </label>
          <label class="date-control">
            <span>${el.search.dateTo}</span>
            <input
              type="date"
              name="to"
              @input=${this._onDateChange}
              @change=${this._onDateChange}
            />
          </label>
        </fieldset>
      </form>

      ${this._stateTemplate()}
    `;
  }

  private _stateTemplate() {
    // Every state renders inside the shared reserved-footprint block so the
    // form above never resizes between them.
    if (this.status === "idle") {
      return html`
        <div class="centered-state state-block">
          <p class="empty" role="status">${el.search.prompt}</p>
        </div>
      `;
    }
    if (this.status === "loading") {
      return html`
        <div class="state-block">
          <loading-spinner></loading-spinner>
        </div>
      `;
    }
    if (this.status === "error") {
      return html`
        <div class="centered-state state-block">
          <error-banner .message=${this.errorMessage}></error-banner>
          <button
            class="button button--secondary retry"
            type="button"
            @click=${this._onRetry}
          >
            ${el.ui.retry}
          </button>
        </div>
      `;
    }
    if (this.items.length === 0) {
      return html`
        <div class="centered-state state-block">
          <p class="empty" role="status">${el.search.empty}</p>
        </div>
      `;
    }

    return html`
      <ul class="results" role="list">
        ${this.items.map(
          (item) => html`
            <li>
              <content-card
                shape="row"
                href=${
                  item.type === "post"
                    ? `/blog/${item.id}`
                    : `/projects/${item.id}`
                }
                title=${item.title}
                subtitle=${item.subtitle}
                heading-level="2"
                description=${item.description}
                thumbnail-url=${item.thumbnailUrl ?? nothing}
                .badge=${this._badgeLabel(item.type)}
                badge-tone=${item.type === "project" ? "project" : "accent"}
                meta-instant=${item.publishedAt}
                .commentCount=${item.commentCount}
                .favoriteCount=${item.favoriteCount}
              ></content-card>
            </li>
          `,
        )}
      </ul>

      <pager-nav
        ?hasPrevious=${this.paging.hasPrevious}
        ?hasNext=${this.hasNextPage}
        .limit=${this.paging.limit}
        .page=${this.paging.page}
        .total=${this.total}
        .limits=${PAGE_SIZES}
        @sf-pager-prev=${this._onPrevClick}
        @sf-pager-next=${this._onNextClick}
        @sf-limit-change=${this._onLimitChange}
      ></pager-nav>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "search-page": SearchPage;
  }
}
