/**
 * Notifications page — the feed at /notifications. The bell in app-header
 * lands here; the page lists the viewer's unified feed, newest first, with
 * URL-carried cursor paging (the shared UrlCursorPagingController).
 *
 * The floor is AUTHENTICATED: any signed-in user gets the inbox — anonymous
 * → /sign-in redirect, no role gate. The five account audit events stay
 * inside it, role-weighted by the server (non-staff viewers simply get no
 * account items). The server enforces the real authorization regardless.
 *
 * Read state: every item carries the viewer's `read` flag. Unread entries
 * render with the accent bar and a tinted surface, read ones dim; clicking
 * an item marks it read (idempotent POST) and dispatches
 * NOTIFICATIONS_CHANGED_EVENT so the header bell refetches its badge; a
 * "mark all read" button does the same for the whole visible feed.
 *
 * Deletion: one entry at a time (an event row is deleted; a ledger row is
 * dismissed for the viewer, the audit record itself surviving), "clear read"
 * removes every read event row in one press (unread entries always survive),
 * and "delete all" empties the whole feed behind a confirmation.
 *
 * The per-kind item lines and the per-item click/delete flows live in
 * `./ui/notification-items-section.ts`, a section controller that renders
 * through this page's template into the same shadow root; the page keeps the
 * list, the guard, the paging, and the toolbar.
 */

import { consume } from "@lit/context";
import { html, LitElement, unsafeCSS } from "lit";
import { customElement, state } from "lit/decorators.js";

import { NAVIGATE_EVENT, redirect } from "@core/router.js";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import {
  sessionContext,
  type SessionContext,
} from "@features/auth/data-access/session.js";
import { requestSignal } from "@shared/api/request-signal.js";
import { el } from "@shared/catalog/el.js";
import { mapError } from "@shared/utils/format.js";
import {
  DEFAULT_PAGE_SIZE,
  PAGE_SIZES,
  UrlCursorPagingController,
} from "@shared/utils/url-cursor-paging.js";
import "@shared/ui/loading-spinner.js";
import "@shared/ui/error-banner.js";
import "@shared/ui/pager-nav.js";

import styles from "./notifications-page.css?inline";
import {
  clearReadNotifications,
  deleteAllNotifications,
  isAccountEvent,
  listNotifications,
  markAllNotificationsRead,
} from "./data-access/notifications-api.js";
import type { NotificationItem } from "./data-access/types.js";
import { NotificationItemsSection } from "./ui/notification-items-section.js";

/**
 * Dispatched on window after any read mutation (item mark-read or
 * read-all) so the header bell refetches its unread badge (badge freshness
 * rides navigation + session changes + this event, never a timer).
 */
export const NOTIFICATIONS_CHANGED_EVENT = "sf-notifications-changed";

type PageStatus = "loading" | "error" | "success";

const NOTIFICATIONS_PATH = "/notifications";

@customElement("notifications-page")
export class NotificationsPage extends LitElement {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(styles),
  ];

  @consume({ context: sessionContext, subscribe: true })
  protected session?: SessionContext;

  @state() private status: PageStatus = "loading";
  @state() private errorMessage = "";
  @state() private items: NotificationItem[] = [];
  @state() private hasNext = false;

  /** The loaded page's filtered row count (pageInfo.total) — the pager's
   * page-count numerator. Reset on every load start/error. */
  @state() private total = 0;

  @state() private _markAllPending = false;

  /** The "delete every read entry" press. */
  @state() private _clearReadPending = false;

  /** The "delete the whole feed" press. */
  @state() private _deleteAllPending = false;

  /** Non-list error (a failed mark-read, delete, read-all, clear-read or
   * delete-all) — above the list. The items section writes it too; it is the
   * page's shared action-error slot. */
  @state() _readError = "";

  /** The server's continuation cursor for the next page (non-reactive —
   * consumed by the pager click only). */
  private _endCursor: string | null = null;

  /** In-flight request controller — aborted on teardown or newer loads. */
  private _abort: AbortController | null = null;

  /** Latches the first authenticated sync (revalidate re-sets the session
   * object without a status change — no double load). */
  private _started = false;

  /** Tracks whether we already redirected to prevent loops. The items
   * section reads it to skip its navigation on a dead session. */
  _redirected = false;

  /** The URL-carried cursor paging state machine (shared). The page keeps the
   * load; the controller owns the URL sync, the refresh-safe hasPrevious
   * marker, the limit, and the Next/Previous mechanics.
   *
   * syncOnConnect: false — the authenticated latch in updated() owns the
   * first load. With the default connect sync the controller ALSO synced at
   * hostConnected, so every mount double-fetched: the mount-time request
   * (fired before the session was known, so it could 401) was aborted by the
   * latch's request — the NS_BINDING_ABORTED class the admin pages share. */
  private readonly paging = new UrlCursorPagingController(this, {
    path: NOTIFICATIONS_PATH,
    navigateEvent: NAVIGATE_EVENT,
    defaultLimit: DEFAULT_PAGE_SIZE,
    syncOnConnect: false,
    nextURL: (after, limit) =>
      `${NOTIFICATIONS_PATH}?limit=${limit}&after=${after}`,
    limitURL: (limit) => `${NOTIFICATIONS_PATH}?limit=${limit}`,
    onLoad: (after, limit, scrollOnLoad) => {
      void this._load(after, limit, scrollOnLoad);
    },
  });

  /** The per-kind row rendering and the per-item click/delete flows — a
   * section controller that renders through this page's template into the
   * same shadow root (see the module doc). */
  private readonly _itemsSection: NotificationItemsSection =
    new NotificationItemsSection(this);

  connectedCallback() {
    super.connectedCallback();
    this._guard();
  }

  updated() {
    // Re-run the guard on every session change (the admin-users pattern):
    // the @consume subscription re-renders without calling
    // connectedCallback.
    this._guard();
    if (this.session?.state.status === "authenticated" && !this._started) {
      this._started = true;
      this.paging.sync(false);
    }
  }

  disconnectedCallback() {
    this._abort?.abort();
    super.disconnectedCallback();
  }

  /* ── Guard (authenticated floor, no role gate) ────────────────── */

  private _guard() {
    if (this._redirected) return;
    const s = this.session?.state;
    if (!s || s.status === "initializing") return;
    if (s.status === "anonymous") {
      this._redirected = true;
      redirect("/sign-in");
      return;
    }
    if (s.status === "error") {
      this.status = "error";
      this.errorMessage = el.ui.serverConnectionFailed;
      return;
    }
    if (this.status !== "loading" && !this._started) {
      this.status = "loading";
    }
  }

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
    this.errorMessage = "";
    this._readError = "";
    this.total = 0;

    try {
      const page = await listNotifications(
        {
          limit,
          ...(after ? { after } : {}),
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
      if (controller.signal.aborted) return;
      this.errorMessage = mapError(err);
      this.total = 0;
      this.status = "error";
    }
  }

  /* ── Read actions ──────────────────────────────────────────────── */

  /** Flip ONE row to read in place — the server confirmed the mark-read. The
   * unread look (accent bar + tint) and the delete controls derive from the
   * flag, so a stale row would misreport both. The items section calls this
   * once its mark-read POST lands. */
  _markLocalRead(id: string) {
    this.items = this.items.map((item) =>
      item.id === id ? { ...item, read: true } : item,
    );
  }

  /** Drop ONE row once the server's 204 lands — the items section calls this
   * from its per-row delete. */
  _removeItem(id: string) {
    this.items = this.items.filter((i) => i.id !== id);
  }

  private async _onMarkAll() {
    if (this._markAllPending) return;
    this._markAllPending = true;
    this._readError = "";
    try {
      await markAllNotificationsRead();
      this.items = this.items.map((item) => ({ ...item, read: true }));
      this._dispatchChanged();
    } catch (err) {
      this._readError = mapError(err);
    } finally {
      this._markAllPending = false;
    }
  }

  /** Delete every READ event row in one press. The ledger
   * rows and the unread rows stay; the local list drops exactly what the
   * server deleted, so the two never disagree. */
  private async _onClearRead() {
    if (this._clearReadPending) return;
    this._clearReadPending = true;
    this._readError = "";
    try {
      await clearReadNotifications();
      this.items = this.items.filter((i) => !i.read || isAccountEvent(i));
      this._dispatchChanged();
    } catch (err) {
      this._readError = mapError(err);
    } finally {
      this._clearReadPending = false;
    }
  }

  /** Empty the viewer's whole feed in one press. Every event row is deleted
   * and every visible ledger row is dismissed for the viewer; the audit
   * ledger itself keeps its rows. The list is emptied outright — nothing
   * survives server-side, so a reload and the local state cannot disagree. */
  private async _onDeleteAll() {
    if (this._deleteAllPending) return;
    if (!window.confirm(el.notifications.deleteAllConfirm)) return;
    this._deleteAllPending = true;
    this._readError = "";
    try {
      await deleteAllNotifications();
      // A load already in flight snapshots the pre-delete feed: cancel it so
      // its response cannot repaint rows the server just removed. The
      // load's abort path leaves `status` alone, so land the page on the
      // empty state here — a stale spinner would otherwise outlive the load.
      this._abort?.abort();
      this.status = "success";
      // The URL's cursor and page ordinal address rows that no longer exist;
      // a reload must start from the bare list.
      window.history.replaceState({}, "", NOTIFICATIONS_PATH);
      this.items = [];
      this.hasNext = false;
      this._endCursor = null;
      this.total = 0;
      this._dispatchChanged();
    } catch (err) {
      this._readError = mapError(err);
    } finally {
      this._deleteAllPending = false;
    }
  }

  /** Announce a read mutation — the items section calls it after a confirmed
   * mark-read or delete — so the header bell refetches its badge. */
  _dispatchChanged() {
    window.dispatchEvent(new Event(NOTIFICATIONS_CHANGED_EVENT));
  }

  /* ── Paging ─────────────────────────────────────────────────────── */

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
      <section class="notifications-page">
        <header class="notifications-header">
          <h1>${el.notifications.title}</h1>
          ${
            this.status === "success" && this.items.length > 0
              ? html`<div class="notifications-actions">
                  ${
                    this.items.some((i) => i.read && !isAccountEvent(i))
                      ? html`<button
                          type="button"
                          class="button button--secondary button--sm clear-read"
                          ?disabled=${this._clearReadPending}
                          @click=${this._onClearRead}
                        >
                          ${
                            this._clearReadPending
                              ? el.notifications.clearReadPending
                              : el.notifications.clearRead
                          }
                        </button>`
                      : ""
                  }
                  <button
                    type="button"
                    class="button button--secondary button--sm mark-all"
                    ?disabled=${this._markAllPending}
                    @click=${this._onMarkAll}
                  >
                    ${
                      this._markAllPending
                        ? el.notifications.markAllPending
                        : el.notifications.markAll
                    }
                  </button>
                  <button
                    type="button"
                    class="button button--danger button--sm delete-all"
                    ?disabled=${this._deleteAllPending}
                    @click=${this._onDeleteAll}
                  >
                    ${
                      this._deleteAllPending
                        ? el.notifications.deleteAllPending
                        : el.notifications.deleteAll
                    }
                  </button>
                </div>`
              : ""
          }
        </header>

        ${
          this._readError
            ? html`<error-banner .message=${this._readError}></error-banner>`
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
                <p class="notifications-empty" role="status">
                  ${el.notifications.empty}
                </p>
              </div>`
            : ""
        }
        ${
          this.status === "success" && this.items.length > 0
            ? html`
                <ul class="notifications-list" role="list">
                  ${this.items.map((item) => this._itemsSection.template(item))}
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
    "notifications-page": NotificationsPage;
  }
}
