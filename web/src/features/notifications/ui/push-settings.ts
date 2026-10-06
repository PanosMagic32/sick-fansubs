/**
 * Push settings section — the account-page surface that owns this device's
 * web push: the subscribe button (double permission), the per-kind toggles,
 * the visible off switch, and the self-test press that proves the channel
 * works.
 *
 * The two halves are companion section controllers rendering through this
 * element's template into the same shadow root: `ui/push-subscriptions-section.ts`
 * owns the device-capability state and the subscribe/disable/test flows, and
 * `ui/push-preferences-section.ts` owns the per-kind form and its save
 * fan-out. This element keeps the session guard, the stylesheet, and the
 * markup every branch renders.
 *
 * Visibility: nothing for anonymous visitors (the account page is
 * authenticated anyway) and nothing while the session initializes; an
 * environment that cannot push states the UNAVAILABLE LINE instead of
 * vanishing — the section has its own account tab, and a blank tab reads as
 * a broken page rather than as an absent capability.
 *
 * Permission UX: NEVER requested on load — only a user gesture opens the
 * flow. The confirm surface is the NATIVE <dialog>: showModal() supplies the
 * focus trap, focus restore, Escape handling, and the inert backdrop, with an
 * open-attribute fallback for a DOM without the method. The worker's
 * `pushsubscriptionchange` relay (handled by the subscriptions section)
 * resubscribes best-effort while the permission stands, and iOS push works
 * only on installed PWAs, so the hint renders while the app runs in the
 * browser.
 */

import { consume } from "@lit/context";
import { html, LitElement, nothing, unsafeCSS } from "lit";
import { customElement } from "lit/decorators.js";

import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import {
  sessionContext,
  type SessionContext,
  type SessionState,
} from "@features/auth/data-access/session.js";
import { el } from "@shared/catalog/el.js";
import "@shared/ui/loading-spinner.js";

import styles from "./push-settings.css?inline";
import { PushPreferencesSection } from "./push-preferences-section.js";
import { PushSubscriptionsSection } from "./push-subscriptions-section.js";

@customElement("push-settings")
export class PushSettings extends LitElement {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(styles),
  ];

  @consume({ context: sessionContext, subscribe: true })
  protected session?: SessionContext;

  /** The consumed session's state for the section controllers (their host
   * contracts read it; the field itself stays the element's). */
  get sessionState(): SessionState | undefined {
    return this.session?.state;
  }

  /** The device-capability half: capability refinement, the double-permission
   * flow, subscribe/disable, the self-test, and the worker relay. Explicit
   * field types: the two sections reference each other through the host
   * interface, and an inferred field type would be circular. */
  readonly subscriptions: PushSubscriptionsSection =
    new PushSubscriptionsSection(this);

  /** The per-kind form half: the loaded preferences, the unsaved draft, and
   * the save fan-out. */
  readonly preferences: PushPreferencesSection = new PushPreferencesSection(
    this,
  );

  render() {
    const s = this.session?.state;
    // Anonymous visitors see no settings section (the account page is
    // authenticated anyway); initializing renders nothing yet.
    if (!s || s.status !== "authenticated") return html``;

    // Unavailable environments get a LINE, not silence — this section lives
    // on its own tab, and an empty tab reads as a broken page rather than as
    // an absent capability.
    if (!this.subscriptions.supported || this.subscriptions._hidden) {
      return html`
        <section class="push-settings">
          <h2>${el.notifications.pushTitle}</h2>
          <p class="push-note push-unavailable">
            ${el.notifications.pushUnavailable}
          </p>
        </section>
      `;
    }

    if (this.subscriptions.status === "loading") {
      return html`
        <section class="push-settings">
          <h2>${el.notifications.pushTitle}</h2>
          <loading-spinner></loading-spinner>
        </section>
      `;
    }
    if (this.subscriptions.status === "error") {
      return html`
        <section class="push-settings">
          <h2>${el.notifications.pushTitle}</h2>
          <p class="push-error" role="alert">${this.subscriptions.error}</p>
          <button
            type="button"
            class="button button--secondary push-retry"
            @click=${() => this.subscriptions.retry()}
          >
            ${el.ui.retry}
          </button>
        </section>
      `;
    }

    return html`
      <section class="push-settings">
        <h2>${el.notifications.pushTitle}</h2>
        <p class="push-explain">${el.notifications.pushExplain}</p>
        ${
          this.subscriptions._iosHint
            ? html`<p class="push-hint">${el.notifications.pushIOSHint}</p>`
            : ""
        }
        ${
          this.subscriptions.error
            ? html`<p class="push-error" role="alert">
                ${this.subscriptions.error}
              </p>`
            : ""
        }
        ${
          this.subscriptions._denied
            ? html`<p class="push-note push-denied">
                ${el.notifications.pushDenied}
              </p>`
            : this.subscriptions._subscription
              ? html`
                  <div class="push-actions">
                    <button
                      class="button button--secondary push-off"
                      type="button"
                      .disabled=${this.subscriptions.busy}
                      @click=${this.subscriptions._onDisableClick}
                    >
                      ${
                        this.subscriptions.pending === "unsubscribe"
                          ? el.notifications.pushDisablePending
                          : el.notifications.pushDisable
                      }
                    </button>
                    <button
                      class="button button--secondary push-test"
                      type="button"
                      .disabled=${this.subscriptions.busy}
                      @click=${this.subscriptions._onTestClick}
                    >
                      ${
                        this.subscriptions._testPending === "sending"
                          ? el.notifications.pushTestPending
                          : el.notifications.pushTestButton
                      }
                    </button>
                  </div>
                  <fieldset
                    class="push-kinds"
                    .disabled=${this.subscriptions.pending === "unsubscribe"}
                  >
                    <legend class="sr-only">
                      ${el.notifications.pushKindsLabel}
                    </legend>
                    ${this.preferences.template()}
                  </fieldset>
                  <!-- The save control appears only while the form differs
                       from the loaded preferences: one save for every edited
                       kind, and only the edited kinds are PUT. -->
                  ${
                    this.preferences._dirty().length > 0 ||
                    this.preferences.savePending
                      ? html`
                          <div class="push-actions push-save-row">
                            <button
                              type="button"
                              class="button button--primary push-save"
                              .disabled=${this.subscriptions.busy}
                              @click=${this.preferences._onSaveClick}
                            >
                              ${
                                this.preferences.savePending
                                  ? el.notifications.pushSavePending
                                  : el.notifications.pushSave
                              }
                            </button>
                          </div>
                        `
                      : ""
                  }
                `
              : html`
                  <div class="push-actions">
                    <button
                      class="button button--primary push-on"
                      type="button"
                      .disabled=${this.subscriptions.busy}
                      @click=${this.subscriptions._onEnableClick}
                    >
                      ${
                        this.subscriptions.pending === "subscribe"
                          ? el.notifications.pushEnablePending
                          : el.notifications.pushEnable
                      }
                    </button>
                  </div>
                `
        }

        <!-- This region's text changes after an action: stay mounted so the announcement survives the re-render. -->
        <p class="push-test-result" role="status">
          ${this.subscriptions._testResult ?? nothing}
        </p>

        <dialog
          class="push-dialog"
          aria-labelledby="push-dialog-title"
          @cancel=${this.subscriptions._onDialogCancel}
        >
          <h3 id="push-dialog-title">${el.notifications.pushDialogTitle}</h3>
          <p>${el.notifications.pushDialogBody}</p>
          <div class="push-dialog-actions">
            <button
              type="button"
              class="button button--primary"
              @click=${this.subscriptions._onDialogConfirm}
            >
              ${el.notifications.pushDialogConfirm}
            </button>
            <button
              type="button"
              class="button button--secondary"
              @click=${this.subscriptions._onDialogCancel}
            >
              ${el.notifications.pushDialogCancel}
            </button>
          </div>
        </dialog>
      </section>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "push-settings": PushSettings;
  }
}
