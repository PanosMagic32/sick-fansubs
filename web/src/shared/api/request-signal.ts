/**
 * Shared client-side timeout composition.
 *
 * A page-owned AbortController composed with the client deadline
 * (cancellation/timeout explicit per client operation): the page owns
 * cancellation — teardown/supersede aborts the composed signal — while the
 * deadline guards against hung requests and surfaces as
 * RequestTimeoutError. The deadline is 15s: it matches the auth-action
 * timeout and sits under the server's 30s WriteTimeout.
 */

export const API_REQUEST_TIMEOUT_MS = 15_000;

/**
 * Compose a page-owned AbortController with the client timeout: the
 * request aborts on teardown/supersede AND when the 15s bound lapses.
 * The controller alone would defeat the API-default timeout — the page
 * owns cancellation, the endpoint owns the deadline, and the composed
 * signal carries both.
 */
export function requestSignal(controller: AbortController): AbortSignal {
  return AbortSignal.any([
    controller.signal,
    AbortSignal.timeout(API_REQUEST_TIMEOUT_MS),
  ]);
}
