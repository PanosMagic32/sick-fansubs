/**
 * Shared loading spinner — one primitive for every page's loading state.
 *
 * The live-region semantics belong to the component: role="status"
 * announces the loading state to screen readers on mount.
 */

import { html, LitElement, unsafeCSS } from "lit";
import { customElement } from "lit/decorators.js";

import { el } from "@shared/catalog/el.js";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";

import styles from "./loading-spinner.css?inline";

@customElement("loading-spinner")
export class LoadingSpinner extends LitElement {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(styles),
  ];

  render() {
    return html`<p class="loading" role="status">${el.ui.loading}</p>`;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "loading-spinner": LoadingSpinner;
  }
}
