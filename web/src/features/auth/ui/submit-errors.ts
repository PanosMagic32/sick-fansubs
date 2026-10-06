/**
 * Shared submit-error mapping, focus, and 429-cooldown helpers.
 *
 * The three auth forms duplicated _mapSubmitError + _focusFirstInvalid
 * with the same shape; a fourth form (blog) would have copied them again.
 * This module is that shared shape, parameterized by the page's field set:
 *
 *  - mapSubmitError(): classifies a thrown error into per-field messages
 *    plus a general banner, using the Greek catalog only: violation
 *    codes are the public API, and server message text never reaches
 *    the UI. A page may name its own rules through the `violation`
 *    option (field + code → message); an unmapped pair falls back to
 *    the generic code message.
 *  - focusFirstInvalid(): moves focus to the first field with an error.
 *  - startRetryCountdown() + RetryCooldownController: the 429 submit
 *    cooldown — the pure ticker plus the small controller pages
 *    mount so they stay the state owners.
 *
 * The pure functions are plain (no custom element, no directive). The
 * controller is the only Lit-typed member and it is opt-in per page.
 */

import { type ReactiveController, type ReactiveControllerHost } from "lit";

import {
  ApiError,
  NetworkError,
  RequestTimeoutError,
  ValidationError,
} from "@shared/api/client.js";
import {
  el,
  problemMessage,
  retryAfterMessage,
  violationMessage,
} from "@shared/catalog/el.js";

export interface SubmitErrorResult<F extends string> {
  /** Per-field catalog messages keyed by server-known field name. */
  fields: Partial<Record<F, string>>;
  /** General banner message (empty string = none). */
  banner: string;
  /**
   * Seconds from the server's Retry-After header on a 429 rate-limited
   * response. Set only when the header was present —
   * the page starts its submit-cooldown window from this value.
   * The banner text carries the STATIC total; the page owns the ticking.
   */
  retryAfterSeconds?: number;
}

export interface MapSubmitErrorOptions<F extends string> {
  /** Classify a 422 violation field as a server-known form field. */
  serverField: (field: string) => field is F;
  /** Fired for the already-authenticated 403 (stale client session). */
  onAlreadyAuthenticated?: () => void;
  /**
   * Page-specific message for one violation (field + code). Return
   * undefined to fall back to the generic violation-code message — the
   * register form names each of its rules this way.
   */
  violation?: (field: string, code: string) => string | undefined;
  /**
   * Page-specific ApiError override (e.g. the account page's wrong-current-
   * password field error). Return null to use the generic mapping.
   */
  specialApiError?: (err: ApiError) => SubmitErrorResult<F> | null;
}

/**
 * Map a thrown submit error to field errors + a banner.
 * Handles: 422 validation, timeout aborts, network failures, problem
 * responses (with the Retry-After suffix), and unknown errors.
 */
export function mapSubmitError<F extends string>(
  err: unknown,
  opts: MapSubmitErrorOptions<F>,
): SubmitErrorResult<F> {
  if (err instanceof ValidationError) {
    const fields: Partial<Record<F, string>> = {};
    let general = "";
    for (const v of err.violations) {
      const message =
        opts.violation?.(v.field, v.code) ?? violationMessage(v.code);
      if (opts.serverField(v.field)) {
        fields[v.field] = message;
      } else {
        // Unknown field (e.g. "general") — show as a banner.
        general = message;
      }
    }
    return {
      fields,
      banner:
        general ||
        (Object.keys(fields).length === 0
          ? problemMessage("/problems/validation")
          : ""),
    };
  }

  // AbortError = the page's own teardown abort; RequestTimeoutError = the
  // composed client deadline. Both get the same UX as a connection
  // failure: generic banner, form stays usable.
  if (err instanceof DOMException && err.name === "AbortError") {
    return { fields: {}, banner: el.ui.error };
  }
  if (err instanceof NetworkError || err instanceof RequestTimeoutError) {
    return { fields: {}, banner: el.ui.error };
  }

  if (err instanceof ApiError) {
    const special = opts.specialApiError?.(err);
    if (special) return special;

    let banner = problemMessage(err.type);
    let retryAfterSeconds: number | undefined;
    // Surface the server's Retry-After header instead
    // of dropping it. The banner carries the static total; the page reads
    // retryAfterSeconds to run the submit cooldown countdown.
    // Start the cooldown for any 429 carrying the header — even when the
    // body was not a problem document, the transport attaches it.
    if (err.status === 429 && err.retryAfterSeconds !== undefined) {
      retryAfterSeconds = err.retryAfterSeconds;
      banner += " " + retryAfterMessage(err.retryAfterSeconds);
    }
    // Sign-in/register-while-authenticated means the client-side session
    // state is stale (e.g. cross-tab sign-in) — the page re-bootstraps to
    // reconcile.
    if (err.type === "/problems/auth/already-authenticated") {
      opts.onAlreadyAuthenticated?.();
    }
    return { fields: {}, banner, retryAfterSeconds };
  }

  return { fields: {}, banner: el.ui.error };
}

/**
 * Run a per-second countdown for a 429 Retry-After window.
 *
 * Calls onTick immediately with the full window (whole seconds, ceiling),
 * then once per second, and finally with 0 when the window closes. The
 * pages feed the remaining value into the disabled submit button's label
 * so the reason the button is disabled stays visible while it ticks down.
 *
 * Returns a cancel function — pages call it on disconnect so the interval
 * never ticks state on a destroyed element.
 */
export function startRetryCountdown(
  seconds: number,
  onTick: (remaining: number) => void,
): () => void {
  if (seconds <= 0) {
    onTick(0);
    return () => {};
  }
  const until = Date.now() + seconds * 1000;
  const tick = () => {
    const ms = until - Date.now();
    const remaining = ms > 0 ? Math.ceil(ms / 1000) : 0;
    onTick(remaining);
    if (remaining <= 0) clearInterval(id);
  };
  const id = setInterval(tick, 1000);
  tick(); // first value immediately — the label starts at the full window
  return () => clearInterval(id);
}

/**
 * Page-mounted 429 cooldown.
 *
 * The three forms each needed the same wiring — a remaining-seconds state,
 * a cancel-on-disconnect timer, and a start hook — so it lives here as a
 * tiny reactive controller instead of being triplicated. Pages add one
 * field (`new RetryCooldownController(this)`) and read `.left`; the
 * controller owns the ticking and cancels the interval in
 * hostDisconnected so a destroyed page never ticks.
 */
export class RetryCooldownController implements ReactiveController {
  /** Whole seconds left in the Retry-After window (0 = no cooldown). */
  left = 0;

  private _stop: (() => void) | undefined;
  private readonly _host: ReactiveControllerHost;

  constructor(host: ReactiveControllerHost) {
    this._host = host;
    host.addController(this);
  }

  hostDisconnected() {
    this._stop?.();
    this._stop = undefined;
  }

  /**
   * Start (or replace) the cooldown for a Retry-After window. A missing
   * or non-positive window resets the state without a timer.
   */
  start(seconds: number | undefined) {
    this._stop?.();
    this._stop = undefined;
    this.left = 0;
    if (seconds === undefined || seconds <= 0) return;
    this._stop = startRetryCountdown(seconds, (left) => {
      this.left = left;
      this._host.requestUpdate();
    });
  }
}

/**
 * Move focus to the first field with an error, if any.
 * `ids` maps each field name to its input id; `errors` is the page's
 * per-field error record. Called after inputs re-enable (a disabled
 * input cannot receive focus).
 */
export function focusFirstInvalid(
  root: ShadowRoot | null | undefined,
  ids: Record<string, string>,
  errors: Record<string, string | undefined>,
): void {
  for (const field of Object.keys(ids)) {
    if (!errors[field]) continue;
    (root?.querySelector(`#${ids[field]}`) as HTMLInputElement | null)?.focus();
    return;
  }
}
