/**
 * Shared form-field directive.
 *
 * The accepted direction is native form controls plus "a custom Lit
 * directive that binds field state (value, dirty, errors) to reactive
 * properties" once form behavior repeats. It now repeats across every
 * form in the tree — this directive is that shared binding. It renders
 * the standard labeled field: label, input (value bound to the page's
 * reactive state, input events forwarded to the page's state owner), and
 * the per-field error paragraph wired via aria-describedby.
 *
 * The block styles (.field, .field-error, .field-hint) live in
 * shared/ui/forms.css, imported by every component that renders a form
 * control alongside this directive.
 *
 * Every rendered input is `required` — every call site is a required field
 * inside a `novalidate` form. An optional field needs a `required` option
 * here first.
 */

import { html, nothing, type TemplateResult } from "lit";
import { directive, Directive } from "lit/directive.js";

export interface FieldSpec {
  /** Unique input id — also the basis for the error paragraph id. */
  id: string;
  /** Visible label (Greek, from the catalog). */
  label: string;
  /** Input type attribute (text, email, password, …). */
  type: string;
  /** Current value from the page's reactive state. */
  value: string;
  /** Per-field error message to show (undefined = none). */
  error?: string;
  /** Optional static hint rendered under the input (Greek, from the
   * catalog) — e.g. the register email's recovery warning. */
  hint?: string;
  /** Disables the input while the form is submitting. */
  pending: boolean;
  /** Autocomplete hint (omitted when undefined). */
  autocomplete?: string;
  /** autocapitalize hint — "none" for username/email (omitted when undefined). */
  autocapitalize?: string;
  /** spellcheck hint — false for username/email (omitted when undefined). */
  spellcheck?: boolean;
  /** Max length hint (omitted when undefined). */
  maxlength?: number;
  /** Called with the new value on input — the page owns state updates. */
  onInput: (value: string) => void;
}

class FormFieldDirective extends Directive {
  render(spec: FieldSpec): TemplateResult {
    const errorId = `${spec.id}-error`;
    const hintId = `${spec.id}-hint`;
    const describedBy =
      spec.error && spec.hint
        ? `${errorId} ${hintId}`
        : spec.error
          ? errorId
          : spec.hint
            ? hintId
            : nothing;
    return html`
      <div class="field">
        <label for=${spec.id}>${spec.label}</label>
        <input
          id=${spec.id}
          type=${spec.type}
          maxlength=${spec.maxlength ?? nothing}
          .value=${spec.value}
          @input=${(e: InputEvent) =>
            spec.onInput((e.target as HTMLInputElement).value)}
          autocomplete=${spec.autocomplete ?? nothing}
          autocapitalize=${spec.autocapitalize ?? nothing}
          spellcheck=${spec.spellcheck ?? nothing}
          required
          ?disabled=${spec.pending}
          aria-invalid=${spec.error ? "true" : nothing}
          aria-describedby=${describedBy}
        />
        ${
          spec.error
            ? html`<p class="field-error" id=${errorId}>${spec.error}</p>`
            : ""
        }
        ${
          spec.hint
            ? html`<p class="field-hint" id=${hintId}>${spec.hint}</p>`
            : ""
        }
      </div>
    `;
  }
}

/** Render a labeled form field with per-field error wiring. */
export const formField = directive(FormFieldDirective);
