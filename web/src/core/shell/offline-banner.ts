/**
 * Offline banner — the strip the shell renders directly under the header on
 * EVERY route while the browser reports no connection.
 *
 * Driven by `navigator.onLine` only: read on connect (a page opened offline
 * shows the strip at once) and re-read on the window's `online`/`offline`
 * events. No dismiss control, and no service-worker state is consulted —
 * the worker is production-only and its page cache is partial, so the copy
 * states the state without promising that cached pages load.
 *
 * The live region stays mounted and empty while online, so the text is a
 * change inside an existing region rather than a region mounted together
 * with its text. Only the visible strip is conditional.
 */
import { html, LitElement, nothing, unsafeCSS } from "lit";
import { customElement, state } from "lit/decorators.js";

import { el } from "@shared/catalog/el.js";

import styles from "./offline-banner.css?inline";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";

@customElement("offline-banner")
export class OfflineBanner extends LitElement {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(styles),
  ];

  @state() private _offline = false;

  connectedCallback(): void {
    super.connectedCallback();
    this._offline = this._reportsOffline();
    window.addEventListener("online", this._onConnectivityChange);
    window.addEventListener("offline", this._onConnectivityChange);
  }

  disconnectedCallback(): void {
    window.removeEventListener("online", this._onConnectivityChange);
    window.removeEventListener("offline", this._onConnectivityChange);
    super.disconnectedCallback();
  }

  private _reportsOffline(): boolean {
    return !navigator.onLine;
  }

  /**
   * The events are only a wake-up call: the state always comes from the
   * property, which the browser keeps authoritative (a spurious event must
   * not desync the strip).
   */
  private _onConnectivityChange = (): void => {
    this._offline = this._reportsOffline();
  };

  render() {
    // The live region persists across the transition and receives its text
    // when the connection drops — a region mounted together with its text
    // is not reliably announced. The visible strip is absent while online
    // and decorative to AT when present, so the sentence is not read twice;
    // a page LOADED offline carries the sentence in this status region from
    // the first paint, with the strip visual-only.
    return html`
      <p class="sr-only" role="status">
        ${this._offline ? el.offline.message : ""}
      </p>
      ${
        this._offline
          ? html`<div class="banner" aria-hidden="true">
              ${el.offline.message}
            </div>`
          : nothing
      }
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "offline-banner": OfflineBanner;
  }
}
