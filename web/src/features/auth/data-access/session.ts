/**
 * Session state machine — the single source of truth for authentication state.
 *
 * A <session-provider> element wraps the application and uses @lit/context
 * to provide reactive session state and action methods to all descendants.
 *
 * States:
 *   initializing  — bootstrap (GET /api/v1/auth/session) has not completed
 *   anonymous     — no valid session
 *   authenticated — valid session; user + CSRF token are in memory
 *   error         — bootstrap failed; retry via retryBootstrap()
 *
 * CSRF token flow:
 *   1. sign-in / bootstrap returns csrfToken → stored via api.setCSRFToken()
 *   2. sign-out / 401 → cleared via api.clearCSRFToken()
 *   3. The API client attaches it automatically to unsafe requests
 *
 * Usage in index.html:
 *   <session-provider>
 *     <app-shell></app-shell>
 *   </session-provider>
 */

import { createContext, provide } from "@lit/context";
import { LitElement, html } from "lit";
import { customElement } from "lit/decorators.js";

import { el } from "@shared/catalog/el.js";
import * as api from "@shared/api/client.js";

import type { PublicUser } from "./types.js";
import * as authApi from "./auth-api.js";

// ── Types ───────────────────────────────────────────────────────────

/** Possible session states. */
export type SessionState =
  | { status: "initializing" }
  | { status: "anonymous" }
  | {
      status: "authenticated";
      user: PublicUser;
      /** Forced-change flag — the shell routes to /account. */
      mustChangePassword: boolean;
    }
  | { status: "error"; message: string };

/** The value provided via @lit/context — state plus action methods. */
export interface SessionContext {
  state: SessionState;

  /** Sign in with username/email + password. Transitions to authenticated. */
  signIn(identifier: string, password: string): Promise<void>;

  /** Register a new account and auto sign in. Transitions to authenticated. */
  register(username: string, email: string, password: string): Promise<void>;

  /** End the current session. Transitions to anonymous. */
  signOut(): Promise<void>;

  /** End all sessions for the current user. Transitions to anonymous. */
  signOutAll(): Promise<void>;

  /** Change password. Atomically revokes all sessions. Transitions to anonymous. */
  changePassword(currentPassword: string, newPassword: string): Promise<void>;

  /** Retry session bootstrap after an error. Resets to initializing. */
  retryBootstrap(): Promise<void>;

  /**
   * Silently re-check the session against the server (cross-tab reconcile).
   * Never shows loading/error states: the current state stands unless the
   * server confirms a change. Only meaningful from the authenticated state.
   */
  revalidate(): Promise<void>;
}

// ── Context key ─────────────────────────────────────────────────────

/**
 * The session context. The Symbol default is the context's IDENTITY —
 * this @lit/context version matches providers and consumers by reference
 * to the truthy default value (an undefined default would make the
 * decorators fall back to per-instance options objects and never match).
 * Verifiable in the installed @lit/context package:
 * lib/create-context.js — createContext is the
 * identity function (n => n); lib/decorators/consume.js — the consumer is
 * keyed by the context value passed to @consume.
 * Consumers outside a <session-provider> never receive the default: their
 * field stays undefined, which is why consumer types are `session?` and
 * their `!this.session` guards are reachable (the honest type,
 * not a truthy-symbol lie).
 */
export const sessionContext = createContext<SessionContext>(Symbol("session"));

// ── Initial states ──────────────────────────────────────────────────

const INITIAL: SessionState = { status: "initializing" };
const ANONYMOUS: SessionState = { status: "anonymous" };

// ── Provider element ────────────────────────────────────────────────

@customElement("session-provider")
export class SessionProvider extends LitElement {
  /**
   * The provided session context. No stacked @state: @provide notifies
   * subscribers itself and this element's render is a static <slot>.
   */
  @provide({ context: sessionContext })
  private _ctx: SessionContext = this._makeContext(INITIAL);

  /** Monotonic counter to prevent stale responses from overwriting newer state. */
  private _generation = 0;

  /** AbortController for the current bootstrap request (cancelled on disconnect). */
  private _bootstrapAbort: AbortController | null = null;

  // ── Lifecycle ──────────────────────────────────────────────────

  connectedCallback() {
    super.connectedCallback();

    // The latch below exists so a revalidate never mutates a removed
    // element. It must be reset here: a provider can be removed and
    // re-added (e.g. re-parented), and a stale latch would leave
    // revalidate silently dead after the reconnect.
    this._disconnected = false;

    // Register the 401 callback so that any API call returning 401
    // automatically transitions the session to anonymous (no refresh loop).
    // NOTE: password-change wrong-password also returns 401 but does
    // NOT trigger this callback because changePassword() uses
    // suppress401Callback in the API client.
    api.onUnauthenticated(() => {
      this._setState(ANONYMOUS);
    });

    // Cross-tab reconcile: the opaque
    // session cookie is shared across tabs of the same browser profile,
    // so a sign-out in another tab leaves this tab's in-memory state
    // stale. Re-check silently whenever the tab regains focus or becomes
    // visible again.
    window.addEventListener("focus", this._onWindowFocus);
    document.addEventListener("visibilitychange", this._onVisibilityChange);
  }

  /**
   * Bootstrap after the first update. firstUpdated() runs synchronously at
   * the end of Lit's first update cycle (https://lit.dev/docs/components/lifecycle/#firstupdated),
   * so @lit/context's @provide/@consume wiring is guaranteed complete
   * when bootstrap starts.
   */
  protected firstUpdated() {
    this._bootstrap();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    // Cancel any in-flight bootstrap so it doesn't set state on a
    // disconnected element.
    this._bootstrapAbort?.abort();
    this._bootstrapAbort = null;
    window.removeEventListener("focus", this._onWindowFocus);
    document.removeEventListener("visibilitychange", this._onVisibilityChange);
    api.clearOnUnauthenticated();
    this._disconnected = true;
  }

  render() {
    return html`<slot></slot>`;
  }

  /** Expose the current session state for tests and debugging. */
  getState(): SessionState {
    return this._ctx.state;
  }

  // ── Internal helpers ───────────────────────────────────────────

  private _makeContext(state: SessionState): SessionContext {
    return {
      state,
      signIn: this._signIn.bind(this),
      register: this._register.bind(this),
      signOut: this._signOut.bind(this),
      signOutAll: this._signOutAll.bind(this),
      changePassword: this._changePassword.bind(this),
      retryBootstrap: this._retryBootstrap.bind(this),
      revalidate: this._revalidate.bind(this),
    };
  }

  private _setState(state: SessionState) {
    this._ctx = this._makeContext(state);
  }

  /* ── Cross-tab revalidation ── */

  /** True while a silent revalidate is in flight (focus/visibility storms). */
  private _revalidating = false;

  /** Set on disconnect — a revalidate must never touch a removed element. */
  private _disconnected = false;

  private _onWindowFocus = () => {
    void this._revalidate();
  };

  private _onVisibilityChange = () => {
    if (document.visibilityState === "visible") {
      void this._revalidate();
    }
  };

  /**
   * Silent session re-check. Unlike retryBootstrap it never flips to the
   * loading or error states: the current state stands unless the server
   * confirms something else. Only meaningful from the authenticated state
   * (a stale "logged in" is the failure mode being fixed); skipped
   * otherwise.
   *
   * Generation handling is deliberately asymmetric: revalidate does NOT
   * bump _generation (it must never invalidate an in-flight action such as
   * changePassword), but it discards its own result if any action bumped
   * the generation while it was in flight.
   */
  private async _revalidate() {
    if (this._ctx.state.status !== "authenticated" || this._revalidating) {
      return;
    }
    this._revalidating = true;
    try {
      const gen = this._generation;
      const result = await authApi.bootstrapSession({});
      if (this._disconnected || gen !== this._generation) return; // superseded
      if (result.authenticated) {
        // Re-asserting "authenticated" is only valid on top of the SAME
        // state. An action (changePassword, sign-out) may have completed
        // while this request was in flight WITHOUT bumping the generation
        // further (actions bump at start, not at completion), so without
        // this check a pre-commit server snapshot could resurrect a
        // just-revoked session.
        if (this._ctx.state.status !== "authenticated") return;
        // An authenticated response always carries csrfToken and user.
        if (!result.csrfToken || !result.user) return; // keep current state
        api.setCSRFToken(result.csrfToken);
        this._setState({
          status: "authenticated",
          user: result.user,
          mustChangePassword: result.mustChangePassword,
        });
      } else {
        api.clearCSRFToken();
        this._setState(ANONYMOUS);
      }
    } catch {
      // Network failure: keep the current state. Focus events must not
      // degrade UX (no error banner, no surprise logout).
    } finally {
      this._revalidating = false;
    }
  }

  // ── Bootstrap ──────────────────────────────────────────────────

  private async _bootstrap() {
    // Cancel any previous in-flight bootstrap.
    this._bootstrapAbort?.abort();
    this._bootstrapAbort = new AbortController();

    const gen = ++this._generation;
    const signal = this._bootstrapAbort.signal;

    try {
      const result = await authApi.bootstrapSession({ signal });

      // Aborted or superseded — discard.
      if (gen !== this._generation || signal.aborted) return;

      if (result.authenticated) {
        // When authenticated, csrfToken and user are always present.
        if (!result.csrfToken || !result.user) {
          this._setState({
            status: "error",
            message: el.ui.invalidServerResponse,
          });
          return;
        }
        api.setCSRFToken(result.csrfToken);
        this._setState({
          status: "authenticated",
          user: result.user,
          mustChangePassword: result.mustChangePassword,
        });
      } else {
        api.clearCSRFToken();
        this._setState(ANONYMOUS);
      }
    } catch (err) {
      if (gen !== this._generation || signal.aborted) return;

      // 401 from bootstrap is unusual (bootstrap always
      // returns 200). If it happens, treat it as unauthenticated rather than
      // an error — the session cookie was likely invalid.
      if (err instanceof api.ApiError && err.status === 401) {
        api.clearCSRFToken();
        this._setState(ANONYMOUS);
        return;
      }

      // Preserve the ApiError requestId for debugging.
      const detail =
        err instanceof api.ApiError ? ` [${err.requestId ?? err.type}]` : "";
      this._setState({
        status: "error",
        message: `${el.ui.serverConnectionFailed}${detail}`,
      });
    }
  }

  private async _retryBootstrap() {
    this._setState(INITIAL);
    await this._bootstrap();
  }

  // ── Actions ────────────────────────────────────────────────────

  // The endpoints own the client-side timeout: auth-api.ts
  // applies AbortSignal.timeout(AUTH_ACTION_TIMEOUT_MS) unless a signal
  // is passed — a stalled connection aborts (~15s) instead of freezing
  // the form until the server's 30s WriteTimeout gives up. The resulting
  // AbortError maps to the generic network banner in the pages' shared
  // submit-error mapper. Bootstrap is the deliberate exception: it passes
  // its own disconnect-abort signal, no client timeout.

  private async _signIn(identifier: string, password: string) {
    const gen = ++this._generation;

    try {
      const result = await authApi.signIn({ identifier, password });

      if (gen !== this._generation) return;

      api.setCSRFToken(result.csrfToken);
      this._setState({
        status: "authenticated",
        user: result.user,
        mustChangePassword: result.mustChangePassword,
      });
    } catch (err) {
      // If we're still initializing after a failure (e.g., sign-in
      // raced with bootstrap and both failed), retry bootstrap so we
      // don't get stuck. Otherwise, re-throw — the previous state stands.
      if (
        gen === this._generation &&
        this._ctx.state.status === "initializing"
      ) {
        this._bootstrap();
      }
      throw err;
    }
  }

  private async _register(username: string, email: string, password: string) {
    const gen = ++this._generation;

    try {
      const result = await authApi.register({
        username,
        email,
        password,
      });

      if (gen !== this._generation) return;

      // Registration never carries the forced-change flag (a new account
      // is created under the user's own password).
      api.setCSRFToken(result.csrfToken);
      this._setState({
        status: "authenticated",
        user: result.user,
        mustChangePassword: false,
      });
    } catch (err) {
      if (
        gen === this._generation &&
        this._ctx.state.status === "initializing"
      ) {
        this._bootstrap();
      }
      throw err;
    }
  }

  private async _signOut() {
    const gen = ++this._generation;

    try {
      await authApi.signOut();
      if (gen !== this._generation) return;
      api.clearCSRFToken();
      this._setState(ANONYMOUS);
    } catch (err) {
      if (
        gen === this._generation &&
        this._ctx.state.status === "initializing"
      ) {
        this._bootstrap();
      }
      throw err;
    }
  }

  private async _signOutAll() {
    const gen = ++this._generation;

    try {
      await authApi.signOutAll();
      if (gen !== this._generation) return;
      api.clearCSRFToken();
      this._setState(ANONYMOUS);
    } catch (err) {
      if (
        gen === this._generation &&
        this._ctx.state.status === "initializing"
      ) {
        this._bootstrap();
      }
      throw err;
    }
  }

  private async _changePassword(currentPassword: string, newPassword: string) {
    const gen = ++this._generation;

    try {
      // suppress401Callback: true — password change returns 401 on wrong
      // current password, not on session expiry.
      // We suppress the global 401→anonymous transition so the user can retry.
      await authApi.changePassword({ currentPassword, newPassword });

      if (gen !== this._generation) return;

      api.clearCSRFToken();
      this._setState(ANONYMOUS);
    } catch (err) {
      if (
        gen === this._generation &&
        this._ctx.state.status === "initializing"
      ) {
        this._bootstrap();
      }
      throw err;
    }
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "session-provider": SessionProvider;
  }
}
