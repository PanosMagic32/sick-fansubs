/**
 * About page — a fully static public page at /about.
 *
 * Copy lives in the catalog (`el.about.*`); the community URLs come from
 * `shared/config/links.ts` (per-environment link config is deferred future
 * work). The member line shows PUBLIC NAMES ONLY — roles are account state
 * and stay off the page. The app-footer owns the version and health lines,
 * so this page deliberately carries neither (single source).
 *
 * No data fetching, no states — the page renders synchronously.
 */
import { LitElement, html, unsafeCSS } from "lit";
import { customElement } from "lit/decorators.js";

import { el } from "@shared/catalog/el.js";
import {
  BUY_ME_A_COFFEE_URL,
  DISCORD_URL,
  FACEBOOK_URL,
  GITHUB_URL,
  TRACKER_URL,
} from "@shared/config/links.js";

import styles from "./about-page.css?inline";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";

interface AboutLink {
  prefix: string;
  label: string;
  suffix: string;
  url: string;
  /** Service tone — selects the row's `.tone-*` class (colors live in the sheet). */
  tone: "facebook" | "discord" | "github" | "tracker" | "coffee";
  /** English labels take `lang="en"` so assistive tech pronounces them in English (the brand names and «Donate»); the lowercase tracker loanword stays plain (the shared catalog's anglicism rule). */
  labelLang?: "en";
}

// Copy (catalog) and URLs (config) paired here in the catalog's group order;
// each tone maps to a `.tone-*` class (colors live in the sheet).
const LINKS: AboutLink[] = [
  {
    prefix: el.about.facebookPrefix,
    label: el.about.facebookLabel,
    suffix: el.about.facebookSuffix,
    url: FACEBOOK_URL,
    tone: "facebook",
    labelLang: "en",
  },
  {
    prefix: el.about.discordPrefix,
    label: el.about.discordLabel,
    suffix: el.about.discordSuffix,
    url: DISCORD_URL,
    tone: "discord",
    labelLang: "en",
  },
  {
    prefix: el.about.githubPrefix,
    label: el.about.githubLabel,
    suffix: el.about.githubSuffix,
    url: GITHUB_URL,
    tone: "github",
    labelLang: "en",
  },
  {
    prefix: el.about.trackerPrefix,
    label: el.about.trackerLabel,
    suffix: el.about.trackerSuffix,
    url: TRACKER_URL,
    tone: "tracker",
  },
  {
    prefix: el.about.donatePrefix,
    label: el.about.donateLabel,
    suffix: el.about.donateSuffix,
    url: BUY_ME_A_COFFEE_URL,
    tone: "coffee",
    labelLang: "en",
  },
];

@customElement("about-page")
export class AboutPage extends LitElement {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(styles),
  ];

  render() {
    return html`
      <article class="about">
        <img
          class="about-logo"
          src="/logo/logo.png"
          alt=""
          role="presentation"
        />

        <h1 class="about-title">${el.about.pageTitle}</h1>
        <p class="about-subtitle">${el.about.pageSubtitle}</p>

        <p class="about-intro">${el.about.intro}</p>

        <p class="about-members">
          <span class="members-label">${el.about.membersLabel}</span>
          <span class="members-names">${el.about.members}</span>
        </p>

        <!-- list-style:none drops list semantics in some browsers; the
             explicit role restores them for assistive tech. -->
        <ul class="about-links" role="list">
          ${LINKS.map(
            (link) => html`
              <li class="link-row tone-${link.tone}">
                <span class="link-desc"
                  >${link.prefix}<a
                    href=${link.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    >${
                      link.labelLang === "en"
                        ? html`<span lang="en">${link.label}</span>`
                        : link.label
                    }</a
                  >${link.suffix}</span
                >
              </li>
            `,
          )}
        </ul>
      </article>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "about-page": AboutPage;
  }
}
