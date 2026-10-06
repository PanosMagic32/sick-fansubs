/**
 * Account sessions section for the account page.
 *
 * The devices panel — the caller's session list (load, load-more, revoke)
 * plus the sign-out actions that live with it — as a reactive controller,
 * so the page keeps ONE shadow root (the panels' aria-labelledby contract
 * and the page's stylesheet require it). The in-flight flag and the action
 * banner are page-shared with the security panel; this section reads and
 * writes them through the host accessors.
 */

import {
  type ReactiveController,
  type ReactiveControllerHost,
  html,
} from "lit";

import { ApiError } from "@shared/api/client.js";
import { requestSignal } from "@shared/api/request-signal.js";
import { el } from "@shared/catalog/el.js";
import { formatDate, mapError } from "@shared/utils/format.js";
import "@shared/ui/error-banner.js";

import type { SessionContext } from "../data-access/session.js";
import {
  listSessions,
  revokeSession,
  SESSION_PAGE_SIZE,
} from "../data-access/sessions-api.js";
import type { SessionItem } from "../data-access/types.js";

/** The host surface the sessions section needs from the account page. */
export interface AccountSessionsSectionHost extends ReactiveControllerHost {
  /** True while the session state machine reports an authenticated session. */
  readonly authed: boolean;
  /** The page's session context — sign-out and the stale-CSRF heal use it. */
  readonly sessionContext: SessionContext | undefined;
  /** The page-shared in-flight flag (also used by the security panel). */
  pending: boolean;
  /** The page-shared action banner (also used by the security panel). */
  actionError: string;
  /** Maps an action failure exactly as the password form does — one shared
   * mapping so a sign-out failure reaches the same field errors, banner,
   * and Retry-After cooldown. */
  applySubmitError(err: unknown): void;
}

export class AccountSessionsSection implements ReactiveController {
  private readonly _host: AccountSessionsSectionHost;

  /** The caller's own unexpired sessions (newest sign-in first). */
  private _sessions: SessionItem[] = [];
  private _sessionsStatus: "loading" | "error" | "success" = "loading";
  /** Load failure — replaces the list with a retry affordance. */
  private _sessionsError = "";
  /** Revoke failure — a banner ABOVE the list; the list stays visible. */
  private _sessionsActionError = "";
  /** The id whose revoke button is in flight. */
  private _revokingSessionId = "";
  /** True while the next page exists (the load-more button). */
  private _sessionsHasNext = false;
  private _sessionsAppending = false;

  /** The current page's continuation cursor (non-reactive). */
  private _sessionsEndCursor: string | null = null;
  /** Set once the first sessions load started — the session's flip to
   * authenticated triggers it once (the profile latch). */
  private _sessionsStarted = false;
  /** In-flight sessions request — aborted on teardown and superseded loads. */
  private _sessionsAbort: AbortController | null = null;
  /** In-flight revoke request — aborted on teardown. */
  private _revokeAbort: AbortController | null = null;

  constructor(host: AccountSessionsSectionHost) {
    this._host = host;
    host.addController(this);
  }

  hostDisconnected() {
    // Cancel an in-flight sessions load so it never sets state on a
    // disconnected element, same for a revoke.
    this._sessionsAbort?.abort();
    this._sessionsAbort = null;
    this._revokeAbort?.abort();
    this._revokeAbort = null;
  }

  hostUpdated() {
    // Fetch the list once the session is authenticated (the profile latch).
    if (this._host.authed && !this._sessionsStarted) {
      this._sessionsStarted = true;
      void this._loadSessions();
    }
  }

  /** The devices tabpanel's body: the session list, the sign-out actions,
   * and their error banner. */
  template() {
    return html`
      ${this._sessionsTemplate()}

      <!-- Sign-out lives HERE, with the devices: the actions address the
           sessions listed above them.
           It is deliberately NOT repeated under every tab. -->
      <div class="signout-actions">
        <button
          type="button"
          class="button button--secondary signout-btn"
          ?disabled=${this._host.pending}
          @click=${this._onSignOut}
        >
          ${el.ui.signOut}
        </button>
        <button
          type="button"
          class="button button--danger signout-all-btn"
          ?disabled=${this._host.pending}
          @click=${this._onSignOutAll}
        >
          ${el.auth.signOutAll}
        </button>
      </div>
      ${
        this._host.actionError
          ? html`<error-banner
              id="account-error"
              .message=${this._host.actionError}
            ></error-banner>`
          : ""
      }
    `;
  }

  /**
   * Load the caller's active sessions (GET /api/v1/users/me/sessions).
   *
   * A 401 here means a genuinely dead session (no suppression): the shared
   * client's global 401 transition flips the session anonymous and the guard
   * redirects.
   */
  private async _loadSessions() {
    if (!this._host.authed) {
      return;
    }

    this._sessionsAbort?.abort();
    const controller = new AbortController();
    this._sessionsAbort = controller;
    this._sessionsStatus = "loading";
    this._sessionsError = "";
    this._sessionsActionError = "";
    this._refresh();

    try {
      const page = await listSessions(
        { limit: SESSION_PAGE_SIZE },
        { signal: requestSignal(controller) },
      );
      if (controller.signal.aborted) return; // torn down or superseded
      this._sessions = page.items;
      this._sessionsHasNext = page.pageInfo.hasNextPage;
      this._sessionsEndCursor = page.pageInfo.endCursor;
      this._sessionsStatus = "success";
    } catch (err) {
      if (controller.signal.aborted) return;
      this._sessionsStatus = "error";
      this._sessionsError = mapError(err);
    }
    this._refresh();
  }

  /** Retry the session list after a load failure. */
  private _onSessionsRetry = () => {
    this._sessionsError = "";
    this._refresh();
    void this._loadSessions();
  };

  /**
   * Append the next page of sessions. The cursor the request was issued with
   * is compared on return: a superseded page (a reload raced ahead) must
   * never append its rows onto a fresh window (the reply-append discipline).
   */
  private _onSessionsMore = async () => {
    const cursor = this._sessionsEndCursor;
    if (!cursor || this._sessionsAppending) return;

    this._sessionsAppending = true;
    this._sessionsActionError = "";
    const controller = new AbortController();
    this._sessionsAbort = controller;

    try {
      const page = await listSessions(
        { limit: SESSION_PAGE_SIZE, after: cursor },
        { signal: requestSignal(controller) },
      );
      if (controller.signal.aborted || this._sessionsEndCursor !== cursor) {
        return;
      }
      this._sessions = [...this._sessions, ...page.items];
      this._sessionsHasNext = page.pageInfo.hasNextPage;
      this._sessionsEndCursor = page.pageInfo.endCursor;
    } catch (err) {
      if (controller.signal.aborted) return;
      this._sessionsActionError = mapError(err);
    } finally {
      this._sessionsAppending = false;
      // In the finally so the early return above still repaints the button.
      this._refresh();
    }
  };

  /**
   * Revoke one session (DELETE /api/v1/users/me/sessions/{id}).
   *
   * The server answers the same idempotent 204 for the caller's own session
   * and for an id that is already gone, so success always removes the row
   * locally. The current row carries no revoke button (it shows the badge),
   * so this never signs the user out from under themselves.
   */
  private async _onSessionRevoke(id: string) {
    if (this._revokingSessionId) return;
    this._revokingSessionId = id;
    this._sessionsActionError = "";
    this._refresh();

    const controller = new AbortController();
    this._revokeAbort = controller;
    try {
      // The composed deadline bounds a stalled DELETE — one hung request
      // must not lock every row's button.
      await revokeSession(id, { signal: requestSignal(controller) });
      if (controller.signal.aborted) return;
      this._sessions = this._sessions.filter((s) => s.id !== id);
    } catch (err) {
      if (controller.signal.aborted) return;
      this._sessionsActionError = mapError(err);
      this._healStaleCSRF(err);
    } finally {
      if (this._revokeAbort === controller) this._revokeAbort = null;
      this._revokingSessionId = "";
    }
    this._refresh();
  }

  private _onSignOut = async () => {
    if (this._host.pending) return;
    this._host.pending = true;
    this._host.actionError = "";
    const session = this._host.sessionContext;
    if (!session) {
      this._host.actionError = el.ui.error;
      this._host.pending = false;
      return;
    }
    try {
      await session.signOut();
      // The provider transitioned to anonymous → the guard redirects.
    } catch (err) {
      this._host.applySubmitError(err);
      this._healStaleCSRF(err);
    } finally {
      this._host.pending = false;
    }
  };

  private _onSignOutAll = async () => {
    if (this._host.pending) return;
    this._host.pending = true;
    this._host.actionError = "";
    const session = this._host.sessionContext;
    if (!session) {
      this._host.actionError = el.ui.error;
      this._host.pending = false;
      return;
    }
    try {
      await session.signOutAll();
      // The provider transitioned to anonymous → the guard redirects.
    } catch (err) {
      this._host.applySubmitError(err);
      this._healStaleCSRF(err);
    } finally {
      this._host.pending = false;
    }
  };

  /**
   * A 403 on a CSRF-requiring action means the in-memory token is stale
   * (cross-tab re-sign-in replaced the session) — silently revalidate to
   * re-sync session + token so a retry works.
   */
  private _healStaleCSRF(err: unknown) {
    if (err instanceof ApiError && err.status === 403) {
      void this._host.sessionContext?.revalidate();
    }
  }

  /**
   * The session list's four states: loading, load error with retry, the
   * rows, and the "no live sessions" copy. A revoke failure renders as a
   * banner ABOVE the rows (`_sessionsActionError`) so the list the user
   * acted on stays visible.
   */
  private _sessionsTemplate() {
    if (this._sessionsStatus === "loading") {
      return html`<div class="state-block">
        <p class="sessions-status" role="status">
          ${el.account.sessionsLoading}
        </p>
      </div>`;
    }
    if (this._sessionsStatus === "error") {
      return html`
        <div class="state-block">
          <error-banner
            id="sessions-error"
            .message=${this._sessionsError}
          ></error-banner>
          <button
            type="button"
            class="button button--secondary button--sm sessions-retry"
            @click=${this._onSessionsRetry}
          >
            ${el.account.sessionsRetry}
          </button>
        </div>
      `;
    }

    return html`
      ${
        this._sessionsActionError
          ? html`<error-banner
              id="session-action-error"
              .message=${this._sessionsActionError}
            ></error-banner>`
          : ""
      }
      ${
        this._sessions.length === 0
          ? html`<div class="state-block">
              <p class="sessions-empty" role="status">
                ${el.account.sessionsEmpty}
              </p>
            </div>`
          : html`
              <ul class="sessions-list" role="list">
                ${this._sessions.map((session) =>
                  this._sessionRowTemplate(session),
                )}
              </ul>
            `
      }
      ${
        this._sessionsHasNext
          ? html`<button
              type="button"
              class="button button--secondary button--sm sessions-more"
              ?disabled=${this._sessionsAppending}
              @click=${this._onSessionsMore}
            >
              ${el.account.sessionsMore}
            </button>`
          : ""
      }
    `;
  }

  /** One session row: device label, instants, the current badge, and the
   * revoke action (never offered on the current row — sign-out owns that). */
  private _sessionRowTemplate(session: SessionItem) {
    const device = session.clientLabel ?? el.account.sessionsUnknownDevice;
    return html`
      <li class="session-row" data-session-id=${session.id}>
        <div class="session-info">
          <span class="session-device">${device}</span>
          ${
            session.current
              ? html`<span class="session-badge"
                  >${el.account.sessionsCurrent}</span
                >`
              : ""
          }
        </div>
        <dl class="session-dates">
          <div>
            <dt>${el.account.sessionsStartedAt}</dt>
            <dd>
              <time datetime=${session.createdAt}
                >${formatDate(session.createdAt)}</time
              >
            </dd>
          </div>
          <div>
            <dt>${el.account.sessionsExpiresAt}</dt>
            <dd>
              <time datetime=${session.expiresAt}
                >${formatDate(session.expiresAt)}</time
              >
            </dd>
          </div>
        </dl>
        ${
          session.current
            ? ""
            : html`<button
                type="button"
                class="button button--secondary button--sm session-revoke"
                ?disabled=${this._revokingSessionId !== ""}
                @click=${() => this._onSessionRevoke(session.id)}
              >
                ${
                  this._revokingSessionId === session.id
                    ? el.account.sessionsRevoking
                    : el.account.sessionsRevoke
                }
              </button>`
        }
      </li>
    `;
  }

  /** Request a host re-render after a state change. */
  private _refresh() {
    this._host.requestUpdate();
  }
}
