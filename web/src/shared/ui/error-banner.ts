/**
 * Shared error banner — one primitive for every page's error state.
 *
 * The message comes from the caller (catalog-resolved Greek copy) —
 * the component owns the alert semantics, styling, and the optional retry
 * control. A `retry` banner dispatches a `retry` event; the host owns what
 * a retry means for its surface.
 */

import { html, LitElement, unsafeCSS } from "lit";
import { customElement, property } from "lit/decorators.js";

import sharedStyles from "@core/styles/baseline.css?inline";
import { el } from "@shared/catalog/el.js";
import controlStyles from "@shared/ui/controls.css?inline";

import styles from "./error-banner.css?inline";

@customElement("error-banner")
export class ErrorBanner extends LitElement {
  /** Greek message from the catalog (resolved by the caller). */
  @property() message = "";

  /** Renders the catalog retry control and dispatches `retry` on press. */
  @property({ type: Boolean }) retry = false;

  /** Optional selector for the element that receives focus when retry is
   * pressed. Defaults to the shell's main landmark. */
  @property() retryFocus = "";

  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(styles),
  ];

  render() {
    return html`
      <div class="banner">
        <p class="error" role="alert">${this.message}</p>
        ${
          this.retry
            ? html`<button
                type="button"
                class="button button--secondary button--sm banner-retry"
                @click=${this._onRetry}
              >
                ${el.ui.retry}
              </button>`
            : ""
        }
      </div>
    `;
  }

  private _onRetry = () => {
    // Move focus before starting the retry: a successful retry unmounts this
    // banner, and focus must not fall to the body.
    this._retryTarget()?.focus();
    this.dispatchEvent(new CustomEvent("retry"));
  };

  private _retryTarget(): HTMLElement | null {
    const root = this.getRootNode() as ParentNode;
    if (this.retryFocus) {
      const target = root.querySelector<HTMLElement>(this.retryFocus);
      if (target) return target;
    }
    const localMain = root.querySelector<HTMLElement>("main");
    if (localMain) return localMain;
    const pageHost = root instanceof ShadowRoot ? root.host : null;
    const shellRoot = pageHost?.getRootNode();
    if (shellRoot instanceof ShadowRoot) {
      return shellRoot.querySelector<HTMLElement>("main");
    }
    return null;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "error-banner": ErrorBanner;
  }
}
