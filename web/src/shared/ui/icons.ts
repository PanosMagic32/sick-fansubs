import { html } from "lit";

/**
 * Feather-style SVG icons — 1em size, stroke-based, inherits color via currentColor.
 * All icons are 24×24 viewBox, 2px stroke, round caps/joins.
 */
const icon = (d: string, fill: "none" | "currentColor" = "none") => html`
  <svg
    xmlns="http://www.w3.org/2000/svg"
    width="1em"
    height="1em"
    viewBox="0 0 24 24"
    fill=${fill}
    stroke="currentColor"
    stroke-width="2"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path d=${d} />
  </svg>
`;

// The Feather heart path — one path constant so the outline and filled
// hearts can never diverge in geometry. Exported for the comments feature's
// `<3` emoticon glyph, which fills the same geometry in the emoji red
// instead of drawing a stroke.
export const heartPath =
  "M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z";

// The canonical Feather bell, body + clapper, as ONE path constant so the
// outline and filled bells can never diverge in geometry (the heartPath
// precedent). The clapper subpath starts with an absolute moveto, so the
// concatenated d string never depends on the bell body's pen position. The
// follower's bell flips between the two states.
export const bellPath =
  "M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9M13.73 21a2 2 0 0 1-3.46 0";

export const icons = {
  grid: icon("M3 3h7v7H3V3zm11 0h7v7h-7V3zM3 14h7v7H3v-7zm11 0h7v7h-7v-7z"),
  search: icon("M11 19a8 8 0 1 0 0-16 8 8 0 0 0 0 16zm10 2-4.35-4.35"),
  // Compass — leading icon for the external Tracker link (used in the
  // icon-only mid-width band, where text is visually hidden).
  tracker: icon(
    "M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20zm4.24-14.24-2.12 6.36-6.36 2.12 2.12-6.36 6.36-2.12z",
  ),
  external: icon(
    "M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6m4-3h6v6",
  ),
  user: icon(
    "M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2M12 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8z",
  ),
  // Notifications bell — the staff feed link — and its FILLED twin for the
  // detail page's follow state (the bell behaves like the heart pair, same
  // box, same geometry).
  bell: icon(bellPath),
  bellFilled: icon(bellPath, "currentColor"),
  // Two-person glyph for the "Η ομάδα" (about) nav link — the team page.
  users: icon(
    "M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8zm14 10v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75",
  ),
  // Sliders (Feather "sliders") — the staff administration (Διαχείριση)
  // nav link.
  sliders: icon(
    "M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6",
  ),
  sun: icon(
    "M12 1v2m0 18v2M4.22 4.22l1.42 1.42m12.72 12.72 1.42 1.42M1 12h2m18 0h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42M12 17a5 5 0 1 0 0-10 5 5 0 0 0 0 10z",
  ),
  moon: icon("M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"),
  // Content indicators: the heart pair (favorite toggle +
  // count indicators) and the speech bubble (comment count). The two heart
  // states share heartPath, so a heart never changes size when it flips.
  heart: icon(heartPath),
  heartFilled: icon(heartPath, "currentColor"),
  messageSquare: icon(
    "M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z",
  ),
  // ── Action glyphs (owner ruling: "icons instead of
  // text" for delete/edit/update and the same kind of action buttons).
  // Feather geometry, the same 24×24 / 2px / round-cap treatment as above.
  // The member panel uses trash (delete), key (password reset), save (the
  // staged save) and chevronDown (the collapsed card opening); the icon pass
  // added edit (the comment editor), x (remove from favorites) and reply
  // (corner-up-left, the comment reply affordance).
  trash: icon(
    "M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M10 11v6M14 11v6",
  ),
  key: icon(
    "M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.78 7.78 5.5 5.5 0 0 1 7.78-7.78zm0 0L15.5 7.5m0 0 3 3L22 7l-3-3m-3.5 3.5L19 4",
  ),
  edit: icon(
    "M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z",
  ),
  x: icon("M18 6 6 18M6 6l12 12"),
  reply: icon("M9 14 4 9l5-5M20 20v-7a4 4 0 0 0-4-4H4"),
  chevronDown: icon("M6 9l6 6 6-6"),
  save: icon(
    "M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2zM17 21v-8H7v8M7 3v5h8",
  ),
} as const;
