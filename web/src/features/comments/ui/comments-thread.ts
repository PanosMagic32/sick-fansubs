/**
 * Comments thread — the inline thread mounted by the click-to-load
 * <comments-section> wrapper at the end of the blog/project detail pages.
 * One shared element for both content kinds: the sort tabs, the keyset
 * top-level list, the `#comment-<id>` deep-link flow (the notification
 * jump), and the count broadcast the detail-page indicator follows.
 *
 * The thread renders into ONE shadow root: the composers and the per-comment
 * items live in two section controllers (`comment-composers-section.ts`,
 * `comment-items-section.ts`) that render through this element's template.
 * The thread's CSS classes, the `#comment-<id>` anchors, and the arrival
 * scroll all depend on that single tree.
 *
 * Behavior:
 *  - Reads are public; list and count load on connect and re-load on a
 *    kind/content-id change. The count refetch broadcasts
 *    COMMENT_COUNT_CHANGED_EVENT after every successful read.
 *  - Sort state syncs to the URL (`?sort=`, the search-page convention)
 *    via pushState + popstate re-reads. Keyset pages of 20 top-level
 *    comments append through a load-more button that renders ONLY on
 *    hasNextPage.
 *  - Deep links: a `#comment-<id>` fragment becomes the list's `focus`
 *    parameter — the server returns the page containing the target (its
 *    parent's page for a reply). Arrival scrolls to the target (center
 *    block, smooth unless prefers-reduced-motion) with the target chip +
 *    one-shot highlight. A missing target (a hard-deleted anchor returns
 *    page 1) renders the deleted-target notice. Create and reply reuse the
 *    same flow: replaceState the fragment, reload with focus — a jump must
 *    not spam history.
 *  - Comment bodies are text-only Lit bindings (never innerHTML) — the
 *    agreed XSS defense.
 */

import { consume } from "@lit/context";
import { html, LitElement, unsafeCSS, type PropertyValues } from "lit";
import { customElement, property, state } from "lit/decorators.js";

import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import formStyles from "@shared/ui/forms.css?inline";
import { NAVIGATE_EVENT } from "@core/router.js";
import {
  sessionContext,
  type SessionContext,
} from "@features/auth/data-access/session.js";
import { requestSignal } from "@shared/api/request-signal.js";
import { el } from "@shared/catalog/el.js";
import { mapError } from "@shared/utils/format.js";
import {
  COMMENT_COUNT_CHANGED_EVENT,
  type CommentCountChangedDetail,
} from "@shared/events.js";
import "@shared/ui/loading-spinner.js";
import "@shared/ui/error-banner.js";

import {
  COMMENT_PAGE_SIZE,
  commentCount,
  listComments,
  type CommentListParams,
} from "../data-access/comments-api.js";
import type {
  CommentItem,
  CommentKind,
  CommentSort,
} from "../data-access/types.js";
import { CommentComposersSection } from "./comment-composers-section.js";
import { CommentItemsSection } from "./comment-items-section.js";
import styles from "./comments-thread.css?inline";

/** Accepted sort values, in display order. */
const SORTS: CommentSort[] = ["top", "newest", "oldest"];

type ThreadStatus = "loading" | "error" | "success";

@customElement("comments-thread")
export class CommentsThread extends LitElement {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(formStyles),
    unsafeCSS(styles),
  ];

  // No stacked @state — @consume requests updates itself. Optional: outside
  // a <session-provider> the field stays undefined. Public so the section
  // controllers can read it through their host interfaces.
  @consume({ context: sessionContext, subscribe: true })
  session?: SessionContext;

  /** The content kind (the URL segment name). */
  @property() kind: CommentKind = "blog-posts";

  /** The opaque content identifier. */
  @property({ attribute: "content-id" }) contentId = "";

  /** The section controllers (class-field initializers so they register
   * with this element). */
  private readonly _composers: CommentComposersSection =
    new CommentComposersSection(this);
  private readonly _itemsSection: CommentItemsSection = new CommentItemsSection(
    this,
  );

  /** The composers section, exposed to the items section — each comment
   * renders its edit form and its inline reply composer from there. */
  get composers(): CommentComposersSection {
    return this._composers;
  }

  @state() private _status: ThreadStatus = "loading";
  @state() private _errorMessage = "";
  @state() private _items: CommentItem[] = [];
  @state() private _hasNext = false;
  @state() private _count: number | null = null;

  /** The active sort — synced to the `?sort=` URL query (default top). */
  @state() private _sort: CommentSort = "top";

  /** The `#comment-<id>` fragment content, if any (the deep-link target). */
  @state() private _focusId = "";

  /** Whether the last focus load found the target (drives the deleted
   * notice — the client-side fallback check). */
  @state() private _targetPresent = false;

  @state() private _loadingMore = false;
  @state() private _moreError = "";

  /** The server's continuation cursor (non-reactive — the load-more click
   * consumes it). */
  private _endCursor: string | null = null;

  /** In-flight list request — aborted on teardown or newer loads. */
  private _abort: AbortController | null = null;

  /** In-flight count request — superseded by newer refreshes. */
  private _countAbort: AbortController | null = null;

  /** Scroll-to-target latch consumed by the updated() hook. */
  private _pendingScroll = false;

  /** The `#comment-<id>` deep-link target, read by the items section for the
   * target chip and the highlight. */
  get focusId(): string {
    return this._focusId;
  }

  connectedCallback() {
    super.connectedCallback();
    this._syncURL();
    window.addEventListener("popstate", this._onPopState);
    window.addEventListener(NAVIGATE_EVENT, this._onNavigate);
    void this._loadFirst({ focus: this._focusId || undefined, scroll: true });
    void this._loadCount();
  }

  disconnectedCallback() {
    this._abort?.abort();
    this._countAbort?.abort();
    window.removeEventListener("popstate", this._onPopState);
    window.removeEventListener(NAVIGATE_EVENT, this._onNavigate);
    // The sections' own teardown (the heart debouncer, the in-flight reply
    // appends) runs from their hostDisconnected hooks.
    super.disconnectedCallback();
  }

  protected willUpdate(changed: PropertyValues<this>) {
    // kind/contentId changed: reload. In practice the parent detail page
    // swaps the thread out of its template during reloads (remounting it),
    // but an attribute-only change must not show the previous content's
    // comments.
    if ((changed.has("kind") || changed.has("contentId")) && this.hasUpdated) {
      this._syncURL();
      void this._loadFirst({ focus: this._focusId || undefined, scroll: true });
      void this._loadCount();
    }
  }

  protected updated() {
    if (!this._pendingScroll || !this._focusId || !this._targetPresent) return;
    this._pendingScroll = false;
    const target = this.shadowRoot?.getElementById(`comment-${this._focusId}`);
    if (target && typeof target.scrollIntoView === "function") {
      const reduce =
        window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ??
        false;
      target.scrollIntoView({
        behavior: reduce ? "auto" : "smooth",
        block: "center",
      });
    }
  }

  /* ── URL sync (sort + focus) ───────────────────────────────────── */

  private _syncURL() {
    const raw = new URLSearchParams(window.location.search).get("sort");
    this._sort = raw === "newest" || raw === "oldest" ? raw : "top";
    const match = /^#comment-(.+)$/.exec(window.location.hash);
    this._focusId = match?.[1] ?? "";
  }

  /** Back/forward (and hash-only history steps): re-read sort + focus
   * from the URL — the single source of truth (the search-page
   * convention). The browser restores its own scroll position, so these
   * loads never scroll. */
  private _onPopState = () => {
    this._syncURL();
    void this._loadFirst({ focus: this._focusId || undefined });
  };

  /** In-place SPA navigation (navigate()): a same-route jump carrying a
   * new `#comment-<id>` fragment must re-resolve the focus without a
   * remount — pushState never fires popstate. Only a CHANGED fragment
   * reloads; plain navigations do nothing. */
  private _onNavigate = () => {
    const prev = this._focusId;
    this._syncURL();
    if (this._focusId !== prev) {
      void this._loadFirst({ focus: this._focusId || undefined, scroll: true });
    }
  };

  /* ── Data ──────────────────────────────────────────────────────── */

  private async _loadFirst(opts: { focus?: string; scroll?: boolean } = {}) {
    this._abort?.abort();
    // A reload supersedes every in-flight reply append and its per-item
    // state — the window about to land is a different one.
    this._itemsSection.abortReplyPages();
    const controller = new AbortController();
    this._abort = controller;
    this._status = "loading";
    this._errorMessage = "";
    this._moreError = "";

    try {
      const params: CommentListParams = opts.focus
        ? { sort: this._sort, limit: COMMENT_PAGE_SIZE, focus: opts.focus }
        : { sort: this._sort, limit: COMMENT_PAGE_SIZE };
      const page = await listComments(this.kind, this.contentId, params, {
        signal: requestSignal(controller),
      });
      if (controller.signal.aborted) return;
      this._items = page.items;
      this._hasNext = page.pageInfo.hasNextPage;
      this._endCursor = page.pageInfo.endCursor;
      this._status = "success";
      if (opts.focus) {
        this._targetPresent = this._findComment(opts.focus) !== null;
        // Arrival scroll only when the target is actually rendered (the
        // deleted-target fallback shows the notice instead).
        this._pendingScroll = Boolean(opts.scroll && this._targetPresent);
      } else {
        this._targetPresent = false;
      }
    } catch (err) {
      if (controller.signal.aborted) return;
      this._errorMessage = mapError(err);
      this._status = "error";
    }
  }

  private async _loadMore() {
    if (!this._endCursor || this._loadingMore) return;
    this._abort?.abort();
    const controller = new AbortController();
    this._abort = controller;
    this._loadingMore = true;
    this._moreError = "";

    try {
      const page = await listComments(
        this.kind,
        this.contentId,
        { sort: this._sort, limit: COMMENT_PAGE_SIZE, after: this._endCursor },
        { signal: requestSignal(controller) },
      );
      if (controller.signal.aborted) return;
      this._items = [...this._items, ...page.items];
      this._hasNext = page.pageInfo.hasNextPage;
      this._endCursor = page.pageInfo.endCursor;
    } catch (err) {
      if (controller.signal.aborted) return;
      this._moreError = mapError(err);
    } finally {
      this._loadingMore = false;
    }
  }

  async _loadCount() {
    this._countAbort?.abort();
    const controller = new AbortController();
    this._countAbort = controller;
    try {
      const res = await commentCount(this.kind, this.contentId, {
        signal: requestSignal(controller),
      });
      if (controller.signal.aborted) return;
      this._count = res.count;
      // Every consumer of the total (the header, the detail-page indicator)
      // re-syncs from this one dispatch — mount AND post-mutation refetches
      // flow through here.
      window.dispatchEvent(
        new CustomEvent<CommentCountChangedDetail>(
          COMMENT_COUNT_CHANGED_EVENT,
          {
            detail: {
              kind: this.kind,
              contentId: this.contentId,
              count: res.count,
            },
          },
        ),
      );
    } catch {
      // The count is decorative — the title just drops the number.
      this._count = null;
    }
  }

  /** Deep-link jump to a (new) comment: replaceState the fragment, then
   * reload with focus — the same code path as a notification arrival. */
  _jumpTo(focusId: string | null) {
    const url = new URL(window.location.href);
    if (focusId) url.hash = `comment-${focusId}`;
    // A string URL — the history API contract (and the test shims)
    // expect strings, never URL objects.
    window.history.replaceState({}, "", url.toString());
    this._syncURL();
    void this._loadFirst({ focus: this._focusId || undefined, scroll: true });
    void this._loadCount();
  }

  /* ── Comment lookup / local mutation ───────────────────────────── */

  _findComment(id: string): CommentItem | null {
    for (const top of this._items) {
      if (top.id === id) return top;
      for (const r of top.replies ?? []) {
        if (r.id === id) return r;
      }
    }
    return null;
  }

  _patchComment(id: string, patch: (c: CommentItem) => CommentItem) {
    this._items = this._items.map((top) => {
      if (top.id === id) return patch(top);
      if (top.replies?.some((r) => r.id === id)) {
        return {
          ...top,
          replies: top.replies.map((r) => (r.id === id ? patch(r) : r)),
        };
      }
      return top;
    });
  }

  _removeComment(id: string) {
    this._items = this._items
      .filter((top) => top.id !== id)
      .map((top) => {
        const replies = top.replies?.filter((r) => r.id !== id);
        if (!replies || replies.length === (top.replies?.length ?? 0)) {
          return top;
        }
        // A removed reply also leaves the total, so the append label (which
        // shows replyCount) cannot overstate the thread. The server count
        // refetch covers the thread total; this item's own number has no
        // server read here.
        return {
          ...top,
          replies,
          replyCount:
            top.replyCount === undefined
              ? undefined
              : Math.max(0, top.replyCount - 1),
        };
      });
  }

  /* ── Sort ──────────────────────────────────────────────────────── */

  private _sortLabel(s: CommentSort): string {
    if (s === "newest") return el.comments.sortNewest;
    if (s === "oldest") return el.comments.sortOldest;
    return el.comments.sortTop;
  }

  private _sortTemplate() {
    return html`
      <fieldset class="ct-sort">
        <legend class="sr-only">${el.comments.sortLabel}</legend>
        ${SORTS.map(
          (s) => html`
            <label class="ct-sort-option">
              <input
                type="radio"
                name="comment-sort"
                value=${s}
                ?checked=${this._sort === s}
                @change=${this._onSortChange}
              />
              <span>${this._sortLabel(s)}</span>
            </label>
          `,
        )}
      </fieldset>
    `;
  }

  private _onSortChange = (e: Event) => {
    const sort = (e.target as HTMLInputElement).value as CommentSort;
    if (sort === this._sort) return;
    const url = new URL(window.location.href);
    if (sort === "top") url.searchParams.delete("sort");
    else url.searchParams.set("sort", sort);
    window.history.pushState({}, "", url.toString());
    this._syncURL();
    // A still-present fragment target re-resolves under the new sort
    // (the server computes the focus page per sort); the chip follows, but
    // there is no arrival scroll on a sort change.
    void this._loadFirst({ focus: this._focusId || undefined });
  };

  /* ── Render ─────────────────────────────────────────────────────── */

  private _stateTemplate() {
    if (this._status === "loading") {
      return html`<loading-spinner></loading-spinner>`;
    }
    if (this._status === "error") {
      return html`
        <div class="ct-centered">
          <error-banner .message=${this._errorMessage}></error-banner>
          <button
            class="button button--secondary ct-retry"
            type="button"
            @click=${() =>
              this._loadFirst({ focus: this._focusId || undefined })}
          >
            ${el.ui.retry}
          </button>
        </div>
      `;
    }
    if (this._items.length === 0) {
      return html`<p class="ct-empty" role="status">${el.comments.empty}</p>`;
    }
    const actionError = this._itemsSection.actionError;
    return html`
      ${
        actionError
          ? html`<error-banner .message=${actionError}></error-banner>`
          : ""
      }
      <div class="ct-thread">
        ${this._items.map((c) => this._itemsSection.commentTemplate(c))}
      </div>
      ${
        this._moreError
          ? html`<p class="ct-form-error" role="alert">${this._moreError}</p>`
          : ""
      }
      ${
        this._hasNext
          ? html`<button
              type="button"
              class="button button--secondary button--sm ct-more"
              ?disabled=${this._loadingMore}
              @click=${this._loadMore}
            >
              ${
                this._loadingMore
                  ? el.comments.loadingMore
                  : this._loadMoreLabel()
              }
            </button>`
          : ""
      }
    `;
  }

  /** The load-more affordance is sort-aware — the next page's direction
   * differs per tab. */
  private _loadMoreLabel(): string {
    if (this._sort === "oldest") return el.comments.loadMoreOldest;
    if (this._sort === "newest") return el.comments.loadMoreNewest;
    return el.comments.loadMoreTop;
  }

  render() {
    return html`
      <section class="comments">
        <header class="ct-header">
          <h2 class="ct-title">
            ${el.comments.title}${
              this._count !== null ? ` (${this._count})` : ""
            }
          </h2>
          ${this._sortTemplate()}
        </header>

        ${
          this._status === "success" && this._focusId && !this._targetPresent
            ? html`<p class="ct-deleted-notice" role="status">
                ${el.comments.deletedNotice}
              </p>`
            : ""
        }
        ${this._composers.composerTemplate()} ${this._stateTemplate()}
      </section>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "comments-thread": CommentsThread;
  }
}
