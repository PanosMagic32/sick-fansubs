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
 * The host carries `hidden` while online: the `:host([hidden])` rule in
 * the stylesheet takes it out of the layout and the accessibility tree
 * (the author `:host` display would otherwise beat the UA sheet).
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

  protected willUpdate(): void {
    this.toggleAttribute("hidden", !this._offline);
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
    if (!this._offline) return nothing;
    // role="status" is the ruled markup: a strip that appears mid-session
    // is announced by assistive tech that tracks live regions, while a page
    // LOADED offline carries the strip from the first paint and it is
    // simply part of the reading order there.
    return html`<div class="banner" role="status">${el.offline.message}</div>`;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "offline-banner": OfflineBanner;
  }
}
