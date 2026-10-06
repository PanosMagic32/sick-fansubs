/**
 * Shared staff-page base.
 *
 * AdminListBase (the blog/projects lists), the users page, and the metrics
 * page share three things through this base:
 *   - the session guard — anonymous → /sign-in, below-moderator → forbidden,
 *     session error → the error banner;
 *   - the authenticated first-load latch in updated() (a mount-time sync would
 *     double-fetch and could fire before the session context exists);
 *   - the load-error mapping — 403 → forbidden, anything else → the catalog's
 *     connection-failure copy.
 *
 * What is NOT shared here: the paging controllers and the per-page DTO state
 * — those stay with each page.
 *
 * The client floor is VISIBILITY ONLY, but it is load-bearing: a below-floor
 * viewer never asks the server. The default is therefore the moderator+
 * floor the server enforces (`identity.CanViewMetrics` / `CanViewStaffList`),
 * and an override is legitimate ONLY with a matching server-side capability
 * check — the logs viewer narrows to super-admin because
 * `identity.CanViewLogs` does. A predicate narrower than the server's would strand a real viewer on
 * the forbidden state with no request to correct it. Pinned by
 * admin-staff-page-base.test.ts; the 403 branch stays for a server-side
 * floor change.
 */

import { consume } from "@lit/context";
import { LitElement } from "lit";
import { state } from "lit/decorators.js";

import { redirect } from "@core/router.js";
import {
  sessionContext,
  type SessionContext,
} from "@features/auth/data-access/session.js";
import { ApiError } from "@shared/api/client.js";
import { el } from "@shared/catalog/el.js";
import { mapError } from "@shared/utils/format.js";
import { isStaffRole } from "@shared/utils/roles.js";

export type PageStatus = "loading" | "error" | "forbidden" | "success";

export abstract class AdminStaffPageBase extends LitElement {
  @consume({ context: sessionContext, subscribe: true })
  protected session?: SessionContext;

  @state() protected status: PageStatus = "loading";
  @state() protected errorMessage = "";

  /** In-flight request controller — aborted on teardown or a newer load. */
  protected _abort: AbortController | null = null;

  /** Latches the first authenticated load (revalidate re-sets the session
   * object without a status change — no double fetch). */
  protected _started = false;

  /** Tracks whether we already redirected to prevent loops. */
  protected _redirected = false;

  connectedCallback() {
    super.connectedCallback();
    this._guard();
  }

  updated(changed: Parameters<LitElement["updated"]>[0]) {
    super.updated(changed);
    // Re-run the guard on every session change: the @consume subscription
    // re-renders without calling connectedCallback again.
    this._guard();
    const s = this.session?.state;
    if (
      s?.status === "authenticated" &&
      this._hasStaffAccess(s.user.role) &&
      !this._started
    ) {
      this._started = true;
      this._start();
    }
  }

  disconnectedCallback() {
    this._abort?.abort();
    super.disconnectedCallback();
  }

  /** The page's first authenticated load — called exactly once. */
  protected abstract _start(): void;

  /**
   * The client floor. Moderator+ by default — the same floor the server
   * enforces for every surface on this dashboard; override only with a
   * matching server-side capability check.
   */
  protected _hasStaffAccess(role: string): boolean {
    return isStaffRole(role);
  }

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
    if (!this._hasStaffAccess(s.user.role)) {
      this.status = "forbidden";
      return;
    }
    if (this.status !== "loading" && !this._started) {
      this.status = "loading";
    }
  }

  /**
   * Starts a load: supersedes the previous request, clears the error slot,
   * and shows the spinner. The caller keeps its own try/catch and applies the
   * result — the DTO state is per page.
   */
  protected _beginLoad(): AbortController {
    this._abort?.abort();
    const controller = new AbortController();
    this._abort = controller;
    this.status = "loading";
    this.errorMessage = "";
    return controller;
  }

  /** Maps a failed load: 403 → forbidden, everything else → the error
   * banner copy. An aborted (superseded/left) request changes nothing. */
  protected _failLoad(err: unknown, controller: AbortController): void {
    if (controller.signal.aborted) return;
    if (err instanceof ApiError && err.status === 403) {
      this.status = "forbidden";
      return;
    }
    this.errorMessage = mapError(err);
    this.status = "error";
  }
}
