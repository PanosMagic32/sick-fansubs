/**
 * The typed door to the shared control vocabulary (`controls.css`): one
 * place that knows the class names, so a variant never rides a typo.
 *
 * `buttonClass()` renders the class list for a native `<button>` or `<a>`.
 * The element keeps its semantics — `type="submit"` on form submits,
 * `href` on links, `disabled`/`aria-*` untouched — the helper only names
 * the look (see docs/patterns/web/components.md for why no shadow-DOM
 * button element exists).
 */

/** The variant vocabulary, in declaration order (controls.css). */
export const BUTTON_VARIANTS = [
  "primary",
  "secondary",
  "danger",
  "ghost",
  "link",
] as const;

export type ButtonVariant = (typeof BUTTON_VARIANTS)[number];

export interface ButtonClassOptions {
  /** Exactly one semantic variant; omit only for a bare `.button`. */
  variant?: ButtonVariant;
  /** Icon-only control: `--icon` keeps the 2rem target floor. */
  icon?: boolean;
  /** Compact size; never shrinks an icon control below its 2rem target. */
  sm?: boolean;
  /** Component-local class(es) appended after the vocabulary. */
  extra?: string;
}

export function buttonClass({
  variant,
  icon,
  sm,
  extra,
}: ButtonClassOptions = {}): string {
  const classes = ["button"];
  if (variant) classes.push(`button--${variant}`);
  if (icon) classes.push("button--icon");
  if (sm) classes.push("button--sm");
  if (extra) classes.push(extra);
  return classes.join(" ");
}
