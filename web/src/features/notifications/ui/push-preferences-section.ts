/**
 * Push preferences section of <push-settings> — the per-kind toggle form:
 * the loaded preferences, the unsaved draft the checkboxes edit, and the
 * save fan-out that writes exactly the changed kinds.
 *
 * A reactive section controller: the element owns the shadow-root markup and
 * maps this section's row template over the loaded list; the section owns the
 * preference state, the draft rules, and the save handlers. Every state
 * change asks the host for a re-render.
 *
 * The checkboxes are a FORM: nothing is written until «Αποθήκευση», and only
 * the kinds whose draft differs from the loaded value are PUT — the endpoint
 * is per-kind, so the client never re-sends an untouched toggle.
 */

import {
  html,
  type ReactiveController,
  type ReactiveControllerHost,
} from "lit";

import { el } from "@shared/catalog/el.js";
import { mapError } from "@shared/utils/format.js";

import { setNotificationPreference } from "../data-access/push-api.js";
import type {
  NotificationPreference,
  PushNotificationKind,
} from "../data-access/types.js";
import type { PushSubscriptionsSurface } from "./push-subscriptions-section.js";

/**
 * Host contract for the preferences section: the subscriptions half owns the
 * shared error banner and the reload the retry control runs through.
 */
export interface PushPreferencesHost extends ReactiveControllerHost {
  readonly subscriptions: PushSubscriptionsSurface;
}

export class PushPreferencesSection implements ReactiveController {
  /** The loaded preferences — the draft is compared against these. */
  private _prefs: NotificationPreference[] = [];

  /** A save fan-out in flight (every control is gated while set). */
  private _savePending = false;

  /** A save fan-out in flight — read by the subscriptions half's busy
   * predicate and the element's controls. */
  get savePending(): boolean {
    return this._savePending;
  }

  /** Unsaved toggle edits, by kind — the checkboxes are a FORM, so nothing
   * is written until «Αποθήκευση» and only the kinds that actually differ
   * are PUT. `_prefs` keeps the loaded value the draft is compared against. */
  private _draft: Partial<Record<PushNotificationKind, boolean>> = {};

  private readonly _host: PushPreferencesHost;

  constructor(host: PushPreferencesHost) {
    this._host = host;
    host.addController(this);
  }

  /** The section owns no listeners or timers; the hook keeps the class
   * structurally assignable to the all-optional ReactiveController
   * interface. */
  hostConnected() {}

  /** Replace the loaded preferences (the subscriptions half fetches them
   * beside the device list) and re-render. */
  applyLoaded(preferences: NotificationPreference[]) {
    this._prefs = preferences;
    this.refresh();
  }

  /** The kind rows — one toggle per loaded preference, mapped by the element
   * inside its fieldset. */
  template() {
    return this._prefs.map(
      (p) => html`
        <label class="push-kind">
          <input
            type="checkbox"
            .checked=${this._shownValue(p)}
            .disabled=${
              this._savePending || this._host.subscriptions.pending !== "none"
            }
            @change=${(e: Event) =>
              this._onKindToggle(
                p.kind,
                (e.target as HTMLInputElement).checked,
              )}
          />
          <span> ${this._kindLabel(p.kind)} </span>
        </label>
      `,
    );
  }

  /** The kinds whose draft differs from the loaded value — the save button's
   * visibility and the PUT fan-out both read this ONE list, so a toggle
   * flipped back to its loaded value simply stops being dirty. */
  _dirty(): NotificationPreference[] {
    return this._prefs.filter(
      (p) =>
        this._draft[p.kind] !== undefined &&
        this._draft[p.kind] !== p.pushEnabled,
    );
  }

  /** The catalog label for a preference kind — the seven shipped kinds. */
  private _kindLabel(kind: PushNotificationKind): string {
    switch (kind) {
      case "heart":
        return el.notifications.pushKindHeart;
      case "comment_reply":
        return el.notifications.pushKindReply;
      case "comment":
        return el.notifications.pushKindComment;
      case "content_updated":
        return el.notifications.pushKindUpdated;
      case "new_content":
        return el.notifications.pushKindNewContent;
      case "comment_removed":
        return el.notifications.pushKindRemoved;
      case "draft_activity":
        return el.notifications.pushKindDraft;
    }
  }

  private _onKindToggle(kind: PushNotificationKind, enabled: boolean) {
    if (this._savePending) return;
    this._host.subscriptions.error = "";
    this._draft = { ...this._draft, [kind]: enabled };
    this.refresh();
  }

  /** The value a checkbox shows: the pending edit, else the loaded value. */
  private _shownValue(p: NotificationPreference): boolean {
    return this._draft[p.kind] ?? p.pushEnabled;
  }

  /** Save the edited kinds — ONE PUT per changed kind (the client never
   * re-sends an untouched toggle). A kind whose PUT failed keeps its draft,
   * so the button stays and a retry re-sends exactly the leftovers. */
  _onSaveClick = async () => {
    const changed = this._dirty();
    if (changed.length === 0 || this._savePending) return;
    this._savePending = true;
    this._host.subscriptions.error = "";
    this.refresh();
    try {
      const results = await Promise.allSettled(
        changed.map((p) =>
          setNotificationPreference(p.kind, this._draft[p.kind] ?? false),
        ),
      );
      let prefs = this._prefs;
      const draft = { ...this._draft };
      results.forEach((result, index) => {
        const kind = changed[index]?.kind;
        if (kind === undefined) return;
        if (result.status === "fulfilled") {
          const value = draft[kind] ?? false;
          prefs = prefs.map((p) =>
            p.kind === kind ? { ...p, pushEnabled: value } : p,
          );
          delete draft[kind];
        } else {
          this._host.subscriptions.error = mapError(result.reason);
        }
      });
      this._prefs = prefs;
      this._draft = draft;
    } finally {
      this._savePending = false;
      this.refresh();
    }
  };

  /** Ask the host for a re-render — the controller's own state does not
   * schedule one. */
  private refresh() {
    this._host.requestUpdate();
  }
}
