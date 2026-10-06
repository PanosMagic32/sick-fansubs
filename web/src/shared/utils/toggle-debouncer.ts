/**
 * ToggleDebouncer — the shared trailing-edge debounce for the app's heart
 * toggles (owner ruling: every like, on any page, uses the same
 * mechanism). Consumers: the comment thread
 * hearts (one timer per comment id) and the detail-page favorite heart
 * (one fixed key).
 *
 * Mechanics: schedule() starts or restarts the delay window for a key; the
 * LAST fire scheduled per key wins when the window expires. Distinct keys
 * debounce independently, so hearting comment A and then comment B still
 * delivers two requests. cancel() clears everything (component teardown).
 *
 * The window keeps the button enabled — clicks inside it update the
 * target; the request phase keeps the caller's existing pending-disable
 * (the confirmed-update machine). A burst of N clicks therefore
 * delivers at most one request.
 */

/** The debounce window. 300 ms: imperceptible as a first-click lag, long
 * enough to absorb click-play bursts. Tests capture timers with this
 * exact delay (never change one without the other). */
export const TOGGLE_DEBOUNCE_DELAY_MS = 300;

export class ToggleDebouncer {
  private readonly timers = new Map<string, ReturnType<typeof setTimeout>>();

  private readonly pending = new Map<string, () => void>();

  constructor(private readonly delayMs: number = TOGGLE_DEBOUNCE_DELAY_MS) {}

  /** Schedules (or reschedules) `fire` for `key` — last call per key wins. */
  schedule(key: string, fire: () => void): void {
    this.pending.set(key, fire);
    const prev = this.timers.get(key);
    if (prev !== undefined) clearTimeout(prev);
    this.timers.set(
      key,
      setTimeout(() => {
        this.timers.delete(key);
        const fn = this.pending.get(key);
        this.pending.delete(key);
        // The window only closes with a pending delivery — cancel() clears
        // both maps together, so fn is always present here.
        if (fn) fn();
      }, this.delayMs),
    );
  }

  /** Clears every open window and every pending delivery (teardown). */
  cancel(): void {
    for (const timer of this.timers.values()) clearTimeout(timer);
    this.timers.clear();
    this.pending.clear();
  }
}
