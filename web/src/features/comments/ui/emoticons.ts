/**
 * Comment emoticon rendering — typed ASCII emoticons become inline COLORED
 * SVG glyphs.
 *
 * Why SVG rather than unicode characters: whether an emoji character renders
 * (and in what colors) is decided by the platform font, so the same body
 * looked different per device — the owner could not see several of them at
 * all. These glyphs carry their own fills, so they render identically
 * everywhere, in both themes.
 *
 * Safety (the body stays text-only): nothing here turns user text into markup.
 * The tokenizer returns plain segments and the render helper composes Lit
 * templates around them, so a body still cannot inject HTML — the glyph is a
 * presentation of the typed token, never a parsed string.
 *
 * The stored body stays exactly as typed (no server change). One accepted
 * consequence: selecting a rendered body copies the glyph (which holds no
 * text), not the ASCII token.
 *
 * Two template rules this module deliberately follows:
 *  - SVG FRAGMENTS use Lit's `svg` tag. In a plain `html` template a fragment
 *    rooted at `<circle />` is parsed as HTML, where the self-closing slash is
 *    ignored — the second eye ended up NESTED inside the first.
 *  - A glyph's children end up on their own lines, which leaves a whitespace
 *    text node inside the `<svg>`. SVG renders nothing for it, and it only
 *    surfaces in the paragraph's `textContent` and in a copy — the render
 *    test normalizes whitespace, so a formatter's line breaks cannot fail the
 *    suite.
 *
 * The `ct-emoticon` class is styled by `comments-thread.css` (baseline
 * alignment); the glyphs carry their own size attributes so they stay correct
 * without that rule.
 */

import { html, svg, type TemplateResult } from "lit";

import { el } from "@shared/catalog/el.js";
import { heartPath } from "@shared/ui/icons.js";

/** The ruled token set. Matching is
 * case-insensitive — `:d` and `:p` render as `:D` and `:P` — and tokens are
 * matched LONGEST FIRST, so `:-)` never renders as `:` plus `-)`. */
export type EmoticonName =
  | "smile"
  | "grin"
  | "frown"
  | "wink"
  | "tongue"
  | "heart"
  | "surprised"
  | "unsure";

// ── Glyphs ──────────────────────────────────────────────────────────
// The classic emoji palette, hardcoded on purpose: these are asset colors
// like the icons module's strokes, not theme tokens.

const FACE = "#ffcc4d";
const INK = "#664500";
const ACCENT = "#dd2e44";

const FACE_CIRCLE = svg`<circle cx="12" cy="12" r="11" fill=${FACE} />`;

/** The two dots every face shares (the wink replaces the right one). */
const EYES = svg`<circle cx="8.4" cy="9.8" r="1.5" fill=${INK} /><circle cx="15.6" cy="9.8" r="1.5" fill=${INK} />`;

const SMILE_MOUTH = svg`<path d="M7.2 13.6c1.1 2.4 2.8 3.7 4.8 3.7s3.7-1.3 4.8-3.7" fill="none" stroke=${INK} stroke-width="1.6" stroke-linecap="round" />`;

/** The svg frame shared by every glyph: 1.2em square, colored, and announced
 * by its Greek label (the token carries meaning, so it is not aria-hidden). */
function glyph(inner: TemplateResult, label: string) {
  return html`<svg
    class="ct-emoticon"
    xmlns="http://www.w3.org/2000/svg"
    width="1.2em"
    height="1.2em"
    viewBox="0 0 24 24"
    role="img"
    aria-label=${label}
  >
    ${inner}
  </svg>`;
}

/** One emoticon: its catalogue label, every ruled token that renders it, and
 * the glyph's inner SVG. One table so a label, a token and a glyph can never
 * drift apart. */
interface EmoticonSpec {
  readonly label: string;
  readonly tokens: readonly string[];
  readonly inner: TemplateResult;
}

const EMOTICONS: Record<EmoticonName, EmoticonSpec> = {
  smile: {
    label: el.comments.emoticonSmile,
    tokens: [":)", ":-)", "=)", "=-)"],
    inner: html`${FACE_CIRCLE}${EYES}${SMILE_MOUTH}`,
  },
  grin: {
    label: el.comments.emoticonGrin,
    tokens: [":D", ":-D"],
    inner: html`${FACE_CIRCLE}${EYES}${svg`<path d="M6.6 13.2h10.8c0 3.4-2.4 5.6-5.4 5.6s-5.4-2.2-5.4-5.6z" fill=${INK} />`}`,
  },
  frown: {
    label: el.comments.emoticonFrown,
    tokens: [":(", ":-("],
    inner: html`${FACE_CIRCLE}${EYES}${svg`<path d="M7.2 16.8c1.1-2.4 2.8-3.7 4.8-3.7s3.7 1.3 4.8 3.7" fill="none" stroke=${INK} stroke-width="1.6" stroke-linecap="round" />`}`,
  },
  wink: {
    label: el.comments.emoticonWink,
    tokens: [";)", ";-)"],
    inner: html`${FACE_CIRCLE}${svg`<circle cx="8.4" cy="9.8" r="1.5" fill=${INK} />`}${svg`<path d="M13.8 9.8c1.2-.9 2.4-.9 3.6 0" fill="none" stroke=${INK} stroke-width="1.6" stroke-linecap="round" />`}${SMILE_MOUTH}`,
  },
  tongue: {
    label: el.comments.emoticonTongue,
    tokens: [":P", ":-P"],
    inner: html`${FACE_CIRCLE}${EYES}${SMILE_MOUTH}${svg`<path d="M9.8 15.4h4.4v1.8a2.2 2.2 0 0 1-4.4 0z" fill=${ACCENT} />`}`,
  },
  // A heart is the whole shape — no face circle behind it (the geometry comes
  // from the icons module, so the two can never diverge).
  heart: {
    label: el.comments.emoticonHeart,
    tokens: ["<3"],
    inner: html`${svg`<path d=${heartPath} fill=${ACCENT} />`}`,
  },
  surprised: {
    label: el.comments.emoticonSurprised,
    tokens: [":O", ":-O"],
    inner: html`${FACE_CIRCLE}${EYES}${svg`<ellipse cx="12" cy="16.4" rx="3" ry="2.4" fill=${INK} />`}`,
  },
  unsure: {
    label: el.comments.emoticonUnsure,
    tokens: [":/", ":-/"],
    inner: html`${FACE_CIRCLE}${EYES}${svg`<path d="M8.6 16.4 14 14.8" fill="none" stroke=${INK} stroke-width="1.6" stroke-linecap="round" />`}`,
  },
};

// ── Tokenizer ───────────────────────────────────────────────────────

const escapeRegExp = (value: string) =>
  value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

const TOKEN_PATTERNS = Object.entries(EMOTICONS)
  .flatMap(([name, spec]) =>
    spec.tokens.map((text) => ({ text, name: name as EmoticonName })),
  )
  .sort((a, b) => b.text.length - a.text.length);

const TOKEN_LOOKUP = new Map(
  TOKEN_PATTERNS.map((t) => [t.text.toLowerCase(), t.name]),
);

/** Anchored at the scan position (`y` + lastIndex) so a token is only taken
 * where the cursor is — never by searching the rest of the body. */
const TOKEN_RE = new RegExp(
  TOKEN_PATTERNS.map((t) => escapeRegExp(t.text)).join("|"),
  "iy",
);

/** A URL-looking run is copied verbatim — a token inside a link is part of
 * the link, not a smiley. */
const URL_RE = /(?:https?:\/\/|www\.)\S+/iy;

/** One rendered body piece: literal text, or one emoticon. */
type BodySegment =
  { kind: "text"; value: string } | { kind: "emoticon"; name: EmoticonName };

/**
 * Split a stored comment body into text and emoticon segments. Pure: nothing
 * is dropped — the concatenated text segments plus the matched tokens
 * reproduce the input exactly.
 */
export function tokenizeBody(body: string): BodySegment[] {
  const segments: BodySegment[] = [];
  let text = "";
  let i = 0;

  const flush = () => {
    if (text) {
      segments.push({ kind: "text", value: text });
      text = "";
    }
  };

  while (i < body.length) {
    URL_RE.lastIndex = i;
    const url = URL_RE.exec(body);
    if (url) {
      text += url[0];
      i = URL_RE.lastIndex;
      continue;
    }

    TOKEN_RE.lastIndex = i;
    const token = TOKEN_RE.exec(body);
    const name = token ? TOKEN_LOOKUP.get(token[0].toLowerCase()) : undefined;
    if (token && name) {
      flush();
      segments.push({ kind: "emoticon", name });
      i = TOKEN_RE.lastIndex;
      continue;
    }

    text += body[i];
    i += 1;
  }

  flush();
  return segments;
}

/** The `aria-label` a glyph renders with (catalog copy — root `AGENTS.md` rule 9). */
export function emoticonLabel(name: EmoticonName): string {
  return EMOTICONS[name].label;
}

function emoticonGlyph(name: EmoticonName): TemplateResult {
  const spec = EMOTICONS[name];
  return glyph(spec.inner, spec.label);
}

/**
 * Render a stored body: text stays text (Lit escapes it), each recognized
 * token becomes its colored glyph. Stored bodies render through this
 * tokenized path — never `innerHTML`.
 */
export function commentBodyTemplate(body: string): TemplateResult {
  return html`${tokenizeBody(body).map((segment) =>
    segment.kind === "emoticon" ? emoticonGlyph(segment.name) : segment.value,
  )}`;
}
