/**
 * Shared URL-carried cursor paging state machine — a small reactive
 * controller that owns the repeated wiring while the page stays the state
 * owner:
 *
 *  - The URL is the single source of truth: the `after` cursor, the page
 *    `limit`, and the 1-based page ordinal all ride location.search. Next
 *    pushes `?limit=…&after=…&page=…` (the host builds the cursor URL — it
 *    owns its other filter params); Previous is history.back(), because
 *    the prior cursor already lives in OUR history entry and popstate
 *    re-syncs from the restored URL. The ordinal is never derived from the
 *    cursor: cursors are opaque, so only next() advancing the URL can say
 *    which page is displayed.
 *  - hasPrevious derives from the CURRENT history entry's state marker
 *    (`sfPager`), stamped by next(). Session-history state survives a
 *    reload, so Previous stays usable after refreshing page N; a
 *    shared/deep-linked page-N URL has no marker, so Previous stays
 *    disabled — history.back() could leave the app there. It must NOT
 *    derive from the URL itself.
 *  - A page-size change is ALWAYS a restart at the first page: a cursor is
 *    only meaningful with the same limit, so setLimit() drops `after` and
 *    `page` and replaceState's the new size URL (no new history entry for
 *    a size change).
 *  - popstate (back/forward) and the navigate event (an SPA navigation
 *    landing on the page) both re-sync. The routes controller reuses the
 *    page element across same-route navigations, so connectedCallback
 *    alone is not enough.
 *  - scroll: page turns scroll to the top; popstate-synced loads don't —
 *    the browser owns scroll restoration there.
 *
 * The controller owns the navigation mechanics; the page owns the load
 * (fetch, abort, state painting) via the onLoad callback.
 */

import { type ReactiveController, type ReactiveControllerHost } from "lit";

/**
 * Page sizes the UI offers (the size dropdown): 10 matches the blog list
 * design (1 featured hero + 9 grid cards); 100 is the wire-contract max.
 */
export const PAGE_SIZES = [10, 20, 50, 100] as const;
export const DEFAULT_PAGE_SIZE: (typeof PAGE_SIZES)[number] = PAGE_SIZES[0];

/** Wire-contract upper bound (limit 1..100) — the client normalizes
 * against the same bound so garbage never reaches the API as a page size. */
const MAX_PAGE_SIZE = 100;

export interface UrlCursorPagingOptions {
  /** Page path — a navigate event re-syncs only when its target pathname
   * equals this path (the routes controller matches pathnames, so a
   * detail route or another page must not re-sync this list). */
  path: string;
  /** SPA-navigation event name (core/router.js NAVIGATE_EVENT). */
  navigateEvent: string;
  /** Fallback size when the URL carries no valid limit (the dropdown's default). */
  defaultLimit: number;
  /**
   * When false, hostConnected does NOT sync — the host drives the first
   * load itself (the admin pages latch their load on the session becoming
   * authenticated; an unconditional mount-time sync would double-fetch and
   * could fire before the session exists). Defaults to true: a plain page
   * loads on mount as before.
   */
  syncOnConnect?: boolean;
  /**
   * Called on every sync with the URL's after cursor (null = first page),
   * the normalized limit, and whether the pending load should scroll to
   * the top (false on popstate — the browser owns scroll restoration).
   */
  onLoad: (after: string | null, limit: number, scrollOnLoad: boolean) => void;
  /** URL to push for a page turn (the host adds its own filter params). */
  nextURL: (after: string, limit: number) => string;
  /** URL for a size change — always the FIRST page (no cursor). */
  limitURL: (limit: number) => string;
}

export class UrlCursorPagingController implements ReactiveController {
  /** True when the current history entry was created by our own Next —
   * this is what enables the Previous button, and it survives a refresh.
   * The host renders it reactively. */
  hasPrevious = false;

  /** The cursor the current page was loaded from (read from the URL).
   * Non-reactive by design — it is only consumed by the host's load. */
  get after(): string | null {
    return this._after;
  }

  /** The normalized page size the current page was loaded with (read from
   * the URL, falling back to defaultLimit). */
  get limit(): number {
    return this._limit;
  }

  /** The 1-based page ordinal the current URL states (1 for anything that
   * is not an integer ≥ 1, and always 1 without a cursor). Non-reactive by
   * design — the host renders it after its own load state changes. A
   * hand-edited ordinal beyond the walk is displayed as-is: the ordinal is
   * a label, the cursor decides the rows. */
  get page(): number {
    return this._page;
  }

  private _after: string | null = null;
  private _limit: number;
  private _page = 1;
  private readonly _host: ReactiveControllerHost;
  private readonly _opts: UrlCursorPagingOptions;

  constructor(host: ReactiveControllerHost, opts: UrlCursorPagingOptions) {
    this._host = host;
    this._opts = opts;
    this._limit = opts.defaultLimit;
    host.addController(this);
  }

  hostConnected() {
    window.addEventListener("popstate", this._onPopState);
    window.addEventListener(this._opts.navigateEvent, this._onNavigate);
    if (this._opts.syncOnConnect !== false) {
      this.sync();
    }
  }

  hostDisconnected() {
    window.removeEventListener("popstate", this._onPopState);
    window.removeEventListener(this._opts.navigateEvent, this._onNavigate);
  }

  /**
   * Re-read the `after` cursor, `limit`, and `page` from the URL and load.
   * A URL without a cursor is the first page — an empty `after` counts as
   * absent — and its ordinal is 1 whatever the URL states, because the
   * cursor, not the ordinal, decides the rows; a URL without a valid limit
   * uses the default, and without a valid ordinal 1. hasPrevious follows
   * the CURRENT entry's state marker (see the class doc). scrollOnLoad
   * scrolls to the top after the load: a new page turn should start at the
   * top; popstate passes false (browser restoration).
   */
  sync(scrollOnLoad = true) {
    const params = new URLSearchParams(window.location.search);
    this._after = params.get("after") || null;
    this._limit = this._parseLimit(params.get("limit"));
    this._page = this._after === null ? 1 : this._parsePage(params.get("page"));
    this._setHasPrevious(
      (window.history.state as { sfPager?: boolean } | null)?.sfPager === true,
    );
    this._opts.onLoad(this._after, this._limit, scrollOnLoad);
  }

  /** Next page: push the continuation cursor into the URL as the new
   * position, stamp the ordinal one past the current one, and reload. The
   * sfPager state marker is what shows (and keeps, across refreshes) the
   * Previous button. */
  next(cursor: string) {
    const url = new URL(
      this._opts.nextURL(cursor, this._limit),
      window.location.origin,
    );
    url.searchParams.set("page", String(this._page + 1));
    window.history.pushState(
      { sfPager: true },
      "",
      `${url.pathname}${url.search}`,
    );
    this.sync();
  }

  /** Previous page: the prior cursor lives in OUR history entry (every
   * page turn pushed a URL), so this is exactly history.back() — popstate
   * re-syncs from the restored URL. */
  prev() {
    window.history.back();
  }

  /**
   * Page-size change: replaceState the new size URL WITHOUT a cursor or an
   * ordinal and re-sync — a cursor is only meaningful with the same limit,
   * so a size change is always a restart at the first page. replaceState
   * (not pushState): a size change is not a navigation step, and it also
   * drops the current entry's sfPager marker (we are on the first page
   * again).
   */
  setLimit(limit: number) {
    const url = new URL(this._opts.limitURL(limit), window.location.origin);
    url.searchParams.delete("page");
    window.history.replaceState({}, "", `${url.pathname}${url.search}`);
    this.sync();
  }

  /** A URL limit is valid when it is digits-only in 1..MAX_PAGE_SIZE (the
   * wire contract); anything else falls back to the default — garbage
   * ("10abc", "1e2", "", negatives, overflow) must never be forwarded
   * to the API. A valid but non-offered value (a hand-edited URL like
   * ?limit=7) is kept: the URL stays the source of truth and the size
   * dropdown renders it as an extra option. */
  private _parseLimit(raw: string | null): number {
    if (raw === null || !/^\d+$/.test(raw)) return this._opts.defaultLimit;
    const n = Number(raw);
    if (n < 1 || n > MAX_PAGE_SIZE) return this._opts.defaultLimit;
    return n;
  }

  /** A URL page is valid when it is digits-only and ≥ 1; anything else
   * (absent, trailing junk, fractions, zero, negatives) falls back to 1 —
   * the displayed ordinal must never be a value the pager could not have
   * produced. */
  private _parsePage(raw: string | null): number {
    if (raw === null || !/^\d+$/.test(raw)) return 1;
    const n = Number(raw);
    return n >= 1 ? n : 1;
  }

  private _onPopState = () => {
    this.sync(false);
  };

  private _onNavigate = (e: Event) => {
    const path = (e as CustomEvent<{ path: string }>).detail?.path;
    if (!path) return;
    // The routes controller matches PATHNAMES — compare the target's
    // pathname exactly so a sibling route (e.g. /posts/123 on the /posts
    // list) never re-syncs this page.
    const end = path.search(/[?#]/);
    const pathname = end === -1 ? path : path.slice(0, end);
    if (pathname === this._opts.path) {
      this.sync();
    }
  };

  private _setHasPrevious(v: boolean) {
    if (this.hasPrevious === v) return;
    this.hasPrevious = v;
    this._host.requestUpdate();
  }
}
