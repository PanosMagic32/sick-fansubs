/**
 * Install control — the install affordance shared by the home donation
 * strip and the account page's profile panel.
 *
 * Four states, the last two rendering nothing:
 *   - the browser offered `beforeinstallprompt` → a button that replays the
 *     deferred prompt (single-use, see `./install-prompt.ts`);
 *   - iOS in browser mode → the manual share-menu instruction (iOS never
 *     fires the event);
 *   - the app already runs installed (`display-mode: standalone` or iOS
 *     `navigator.standalone`);
 *   - this browser already allowed notifications (per-device signal).
 *
 * The host carries `hidden` whenever nothing renders, so a host layout can
 * fall back to its single-item shape (donation-banner.css). The hide rule
 * is re-evaluated on the Permissions API's change event, because granting
 * push in the sibling settings section must hide the control without a
 * remount.
 */
import { html, LitElement, nothing, unsafeCSS } from "lit";
import { customElement, state } from "lit/decorators.js";

import { el } from "@shared/catalog/el.js";
import { isIOS, isStandalone } from "@shared/utils/platform.js";

import {
  consumeInstallPrompt,
  installPromptAvailable,
  notificationsGranted,
  subscribeInstallPrompt,
} from "./install-prompt.js";
import styles from "./install-control.css?inline";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";

type InstallBranch = "button" | "ios" | "none";

@customElement("install-control")
export class InstallControl extends LitElement {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(styles),
  ];

  @state() private _available = false;

  /** Which branch renders; mirrored onto the host's `hidden` attribute. */
  @state() private _branch: InstallBranch = "none";

  private _unsubscribe?: () => void;
  private _permissionStatus?: PermissionStatus;

  connectedCallback(): void {
    super.connectedCallback();
    // Re-read on connect: the prompt may have arrived before this control
    // was ever rendered (the strip is home-only; the account page mounts
    // later in the session).
    this._available = installPromptAvailable();
    this._unsubscribe = subscribeInstallPrompt(() => {
      this._available = installPromptAvailable();
    });
    void this._watchNotificationPermission();
  }

  disconnectedCallback(): void {
    super.disconnectedCallback();
    this._unsubscribe?.();
    this._unsubscribe = undefined;
    this._permissionStatus?.removeEventListener(
      "change",
      this._onPermissionChange,
    );
    this._permissionStatus = undefined;
  }

  protected willUpdate(): void {
    this._branch = this._computeBranch();
    this.toggleAttribute("hidden", this._branch === "none");
  }

  private _computeBranch(): InstallBranch {
    // Installed, or already allowing notifications for this browser: stay
    // quiet, whichever branch would have rendered.
    if (isStandalone() || notificationsGranted()) return "none";
    if (this._available) return "button";
    // iOS never fires `beforeinstallprompt`; the manual share-menu path is
    // the only one there.
    if (isIOS()) return "ios";
    return "none";
  }

  /**
   * Re-evaluate when the notification permission changes — granting push
   * in the sibling settings section must hide the control in place. The
   * Permissions API does not expose a "notifications" entry everywhere;
   * without it the control re-evaluates on its next render only.
   */
  private async _watchNotificationPermission(): Promise<void> {
    try {
      const status = await navigator.permissions?.query({
        name: "notifications" as PermissionName,
      });
      if (!this.isConnected || !status) return;
      // A superseded query (fast reconnect) must not leave its listener
      // behind: detach the previous status before adopting this one.
      this._permissionStatus?.removeEventListener(
        "change",
        this._onPermissionChange,
      );
      this._permissionStatus = status;
      status.addEventListener("change", this._onPermissionChange);
    } catch {
      // Unsupported permission name — nothing to watch.
    }
  }

  private _onPermissionChange = (): void => {
    this.requestUpdate();
  };

  private _onInstall = (): void => {
    // A rejected prompt (the browser already consumed the event) leaves
    // nothing to surface — the control hides either way.
    void consumeInstallPrompt().catch(() => {});
  };

  render() {
    switch (this._branch) {
      case "button":
        return html`<button
          class="button button--secondary button--sm install"
          type="button"
          @click=${this._onInstall}
        >
          ${el.install.button}
        </button>`;
      case "ios":
        return html`<span class="install-hint">${el.install.iosHint}</span>`;
      default:
        return nothing;
    }
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "install-control": InstallControl;
  }
}
