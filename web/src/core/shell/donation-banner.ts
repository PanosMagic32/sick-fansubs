/**
 * Donation banner — a slim strip on the HOME ROUTE ONLY (gated on
 * currentPath in the shell): catalog sentence + external link left, the
 * shared install control right.
 */
import { html, LitElement, unsafeCSS } from "lit";
import { customElement } from "lit/decorators.js";

import { el } from "@shared/catalog/el.js";
import { BUY_ME_A_COFFEE_URL } from "@shared/config/links.js";

import "@shared/pwa/install-control.js";
import styles from "./donation-banner.css?inline";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";

@customElement("donation-banner")
export class DonationBanner extends LitElement {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(styles),
  ];

  render() {
    return html`
      <div class="banner">
        <div class="banner-left">
          <p>${el.donation.bannerText}</p>
          <a
            href=${BUY_ME_A_COFFEE_URL}
            target="_blank"
            rel="noopener noreferrer"
            ><span aria-hidden="true">☕</span>
            <span lang="en">${el.about.donateLabel}</span></a
          >
        </div>
        <install-control></install-control>
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "donation-banner": DonationBanner;
  }
}
