/**
 * Account favorites section for the account page.
 *
 * The favorites panel — the posts/projects tab, the URL-carried pager, and
 * the load/remove/retry wiring — lives here as a reactive controller so the
 * page keeps ONE shadow root (the panels' aria-labelledby contract and
 * the page's stylesheet require it). The pager stays the shared
 * `UrlCursorPagingController`, constructed with the page host so its
 * popstate/navigate listeners bind to the page's lifecycle. The page calls
 * `sync()` when the tab becomes active — the load is gated on the active
 * tab.
 */

import {
  type ReactiveController,
  type ReactiveControllerHost,
  html,
} from "lit";

import { NAVIGATE_EVENT } from "@core/router.js";
import { requestSignal } from "@shared/api/request-signal.js";
import { el } from "@shared/catalog/el.js";
import { icons } from "@shared/ui/icons.js";
import { mapError } from "@shared/utils/format.js";
import {
  DEFAULT_PAGE_SIZE,
  PAGE_SIZES,
  UrlCursorPagingController,
} from "@shared/utils/url-cursor-paging.js";
import "@shared/ui/content-card.js";
import "@shared/ui/error-banner.js";
import "@shared/ui/loading-spinner.js";
import "@shared/ui/pager-nav.js";

import { listFavorites, removeFavorite } from "../data-access/favorites-api.js";
import type { FavoriteItem, FavoriteKind } from "../data-access/types.js";

/** The host surface the favorites section needs from the account page. */
export interface AccountFavoritesSectionHost extends ReactiveControllerHost {
  /** True while the session state machine reports an authenticated session. */
  readonly authed: boolean;
}

export interface AccountFavoritesSectionOptions {
  /** True while the favorites tab is the page's active one — the mount-time
   * paging sync and the auth latch must not fetch the list while another
   * tab is active; switching TO the tab syncs explicitly. */
  isActive: () => boolean;
}

export class AccountFavoritesSection implements ReactiveController {
  private readonly _host: AccountFavoritesSectionHost;
  private readonly _isActive: () => boolean;

  /** The URL-carried paging state machine for the favorites list — the
   * controller owns after/limit/hasPrevious; this section owns the tab and
   * the load (the search-page pattern). */
  private readonly favPaging: UrlCursorPagingController;

  /** Active tab: "posts" | "projects" — read from the URL on every sync
   * (the URL is the single source of truth, like the search page's q). */
  private _favTab: "posts" | "projects" = "posts";
  private _favStatus: "loading" | "error" | "success" = "loading";
  private _favItems: FavoriteItem[] = [];
  private _favError = "";
  /** Per-card removal failure (does NOT replace the list). */
  private _favActionError = "";
  /** The id of the card whose remove button is in flight. */
  private _removingId = "";
  /** True while the next page exists (the pager's Next enablement). */
  private _favHasNext = false;

  /** The loaded page's filtered row count (pageInfo.total) — the pager's
   * page-count numerator. Reset on every load start/error. */
  private _favTotal = 0;

  /** The current page's continuation cursor (non-reactive — Next reads it). */
  private _favEndCursor: string | null = null;
  /** The in-flight load's (tab, after, limit) key + controller — the
   * paging controller syncs twice on an authenticated mount (hostConnected
   * + the auth latch in hostUpdated), and the second sync must not abort and
   * restart the identical load (SPA navigation to /account would double
   * fetch). A different key (tab/page/size change) aborts + restarts. */
  private _favInFlight: { key: string; controller: AbortController } | null =
    null;
  /** Set once the first favorites load started — the session's flip to
   * authenticated triggers the load once (same latch idea as the profile). */
  private _favStarted = false;

  constructor(
    host: AccountFavoritesSectionHost,
    opts: AccountFavoritesSectionOptions,
  ) {
    this._host = host;
    this._isActive = opts.isActive;
    host.addController(this);
    this.favPaging = new UrlCursorPagingController(host, {
      path: "/account",
      navigateEvent: NAVIGATE_EVENT,
      defaultLimit: DEFAULT_PAGE_SIZE,
      nextURL: (after, limit) => {
        const p = new URLSearchParams({
          tab: this._favTab,
          limit: String(limit),
          after,
        });
        return `/account?${p.toString()}`;
      },
      limitURL: (limit) => {
        const p = new URLSearchParams({
          tab: this._favTab,
          limit: String(limit),
        });
        return `/account?${p.toString()}`;
      },
      onLoad: (after, limit, scrollOnLoad) => {
        // Gated on the favorites tab: the mount-time
        // controller sync and the auth latch must not fetch the list while
        // another tab is active — switching TO the favorites tab syncs
        // explicitly (the page's tab switch).
        if (!this._isActive()) return;
        const rawTab = new URLSearchParams(window.location.search).get("tab");
        this._favTab = rawTab === "projects" ? "projects" : "posts";
        this._refresh();
        void this._loadFavorites(after, limit, scrollOnLoad);
      },
    });
  }

  hostDisconnected() {
    // Cancel an in-flight favorites load so it never sets state on a
    // disconnected element.
    this._favInFlight?.controller.abort();
    this._favInFlight = null;
  }

  hostUpdated() {
    // The paging controller syncs the URL on mount, but the load itself
    // waits for the session. The latch's load must not scroll: a
    // deep-link/reload first load keeps the browser's restored position —
    // an explicit tab activation and a page turn do scroll (the
    // notifications precedent).
    if (this._host.authed && !this._favStarted) {
      this._favStarted = true;
      this.favPaging.sync(false);
    }
  }

  /** Re-sync the pager with the URL — the page calls this when the
   * favorites tab becomes active (the load is gated on the active tab, so
   * the mount-time sync skipped it); activation scrolls to the top. */
  sync() {
    this.favPaging.sync();
  }

  /** The favorites tabpanel's body. */
  template() {
    return this._favoritesTemplate();
  }

  /** The API kind for the active tab (the tab→kind mapping lives ONCE). */
  private get _favKind(): FavoriteKind {
    return this._favTab === "projects" ? "projects" : "blog-posts";
  }

  /**
   * Load one favorites page for the current tab (the kind maps the tab).
   * A 401 means a genuinely dead session — the global 401 transition
   * flips anonymous and the guard redirects.
   */
  private async _loadFavorites(
    after: string | null,
    limit: number,
    scrollOnLoad: boolean,
  ) {
    if (!this._host.authed) return;

    const key = `${this._favTab}:${after ?? ""}:${limit}`;
    if (this._favInFlight?.key === key) return; // identical load in flight

    this._favInFlight?.controller.abort();
    const controller = new AbortController();
    this._favInFlight = { key, controller };
    this._favStatus = "loading";
    this._favError = "";
    this._favActionError = "";
    this._favTotal = 0;
    this._refresh();

    try {
      const page = await listFavorites(
        this._favKind,
        { limit, ...(after ? { after } : {}) },
        { signal: requestSignal(controller) },
      );
      if (controller.signal.aborted) return; // torn down or superseded
      this._favItems = page.items;
      this._favHasNext = page.pageInfo.hasNextPage;
      this._favEndCursor = page.pageInfo.endCursor;
      this._favTotal = page.pageInfo.total;
      this._favStatus = "success";
      // A page turn starts at the top; popstate-synced loads keep the
      // browser's restored scroll position.
      if (scrollOnLoad) window.scrollTo(0, 0);
    } catch (err) {
      if (controller.signal.aborted) return;
      this._favError = mapError(err);
      this._favTotal = 0;
      this._favStatus = "error";
    } finally {
      if (this._favInFlight?.controller === controller) {
        this._favInFlight = null;
      }
    }
    this._refresh();
  }

  /** Tab change: the tab rides the URL like the search page's q/type. */
  private _onFavTabChange = (e: Event) => {
    const input = e.target as HTMLInputElement;
    const tab = input.value === "projects" ? "projects" : "posts";
    if (tab === this._favTab) return;
    window.history.pushState({}, "", `/account?tab=${tab}`);
    this.favPaging.sync();
  };

  /** Remove one favorite, then reload the current page — the removed item
   * shifts every following cursor position, so a reload (not a local
   * splice) keeps the list consistent with the server. */
  private async _onFavRemove(item: FavoriteItem) {
    if (this._removingId) return; // one removal in flight at a time
    this._removingId = item.id;
    this._favActionError = "";
    this._refresh();
    try {
      await removeFavorite(this._favKind, item.id);
      this.favPaging.sync();
    } catch (err) {
      this._favActionError = mapError(err);
    } finally {
      this._removingId = "";
    }
    this._refresh();
  }

  private _onFavRetry = () => {
    this.favPaging.sync();
  };

  private _onFavNext = () => {
    if (this._favEndCursor) this.favPaging.next(this._favEndCursor);
  };

  private _onFavPrev = () => {
    this.favPaging.prev();
  };

  private _onFavLimitChange = (e: Event) => {
    this.favPaging.setLimit((e as CustomEvent<number>).detail);
  };

  /**
   * Favorites section states: the tab row, then loading / error+retry /
   * empty / the search-style card list with the shared pager. The pager is
   * hidden on loading/error/empty — it renders only with items.
   */
  private _favoritesTemplate() {
    return html`
      <fieldset class="fav-tabs">
        <legend class="sr-only">${el.favorites.tabsLabel}</legend>
        ${(["posts", "projects"] as const).map(
          (t) => html`
            <label class="fav-tab">
              <input
                type="radio"
                name="fav-tab"
                value=${t}
                ?checked=${this._favTab === t}
                @change=${this._onFavTabChange}
              />
              <span>
                ${
                  t === "projects"
                    ? html`<span lang="en">${el.favorites.tabProjects}</span>`
                    : el.favorites.tabPosts
                }
              </span>
            </label>
          `,
        )}
      </fieldset>

      ${
        this._favStatus === "loading"
          ? html`<div class="state-block">
              <loading-spinner></loading-spinner>
            </div>`
          : ""
      }
      ${
        this._favStatus === "error"
          ? html`
              <div class="state-block">
                <error-banner .message=${this._favError}></error-banner>
                <button
                  type="button"
                  class="button button--primary retry-btn"
                  @click=${this._onFavRetry}
                >
                  ${el.ui.retry}
                </button>
              </div>
            `
          : ""
      }
      ${
        this._favStatus === "success" && this._favItems.length === 0
          ? html`<div class="state-block">
              <p class="fav-empty" role="status">
                ${
                  this._favTab === "projects"
                    ? el.favorites.emptyProjects
                    : el.favorites.emptyPosts
                }
              </p>
            </div>`
          : ""
      }
      ${
        this._favStatus === "success" && this._favItems.length > 0
          ? html`
              ${
                this._favActionError
                  ? html`<error-banner
                      .message=${this._favActionError}
                    ></error-banner>`
                  : ""
              }
              <ul class="fav-results" role="list">
                ${this._favItems.map((item) => this._favoriteCardTemplate(item))}
              </ul>
              <pager-nav
                ?hasPrevious=${this.favPaging.hasPrevious}
                ?hasNext=${this._favHasNext}
                .limit=${this.favPaging.limit}
                .page=${this.favPaging.page}
                .total=${this._favTotal}
                .limits=${PAGE_SIZES}
                @sf-pager-prev=${this._onFavPrev}
                @sf-pager-next=${this._onFavNext}
                @sf-limit-change=${this._onFavLimitChange}
              ></pager-nav>
            `
          : ""
      }
    `;
  }

  /** One favorites card: the dense row card linking to the content detail,
   * with the remove control as the card's overlay child (the card owns the
   * corner placement and the text room it reserves). The tab already
   * separates the types, so no badge. */
  private _favoriteCardTemplate(item: FavoriteItem) {
    const href =
      this._favKind === "projects"
        ? `/projects/${item.id}`
        : `/blog/${item.id}`;
    return html`
      <li>
        <content-card
          shape="row"
          dense
          href=${href}
          title=${item.title}
          subtitle=${item.subtitle}
          heading-level="3"
          description=${item.description}
          thumbnail-url=${item.thumbnailUrl}
          meta-instant=${item.publishedAt}
          .commentCount=${item.commentCount}
          .favoriteCount=${item.favoriteCount}
        >
          <button
            slot="overlay"
            type="button"
            class="button button--icon button--secondary fav-remove"
            aria-label=${el.favorites.removeAria}
            title=${el.favorites.removeAria}
            ?disabled=${this._removingId !== ""}
            @click=${() => this._onFavRemove(item)}
          >
            ${icons.x}
          </button>
        </content-card>
      </li>
    `;
  }

  /** Request a host re-render after a state change. */
  private _refresh() {
    this._host.requestUpdate();
  }
}
