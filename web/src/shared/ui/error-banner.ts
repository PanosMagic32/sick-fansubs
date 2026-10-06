/**
 * Shared error banner — one primitive for every page's error state.
 *
 * The message comes from the caller (catalog-resolved Greek copy) —
 * the component only owns the alert semantics and styling.
 */

import { html, LitElement, unsafeCSS } from "lit";
import { customElement, property } from "lit/decorators.js";

import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";

import styles from "./error-banner.css?inline";

@customElement("error-banner")
export class ErrorBanner extends LitElement {
  /** Greek message from the catalog (resolved by the caller). */
  @property() message = "";

  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(styles),
  ];

  render() {
    return html`<p class="error" role="alert">${this.message}</p>`;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "error-banner": ErrorBanner;
  }
}
