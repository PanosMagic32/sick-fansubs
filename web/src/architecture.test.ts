/**
 * Architecture guards — the machine-checked half of the web pattern
 * docs (`docs/patterns/web/components.md`, `css.md`). The guard set is
 * table-driven, every guard self-checks against a bad and a clean snippet
 * before it scans the tree (the `internal/codestyle` shape), and every
 * scan asserts a non-vacuity floor so a rename cannot hollow a guard out.
 *
 * A finding is a human-readable string; an empty list is a pass. Guards
 * scan source text only (happy-dom renders no layout), which is why the
 * CSS rules below are pinned at their sources.
 */

import { describe, expect, test } from "bun:test";

import {
  stripCssComments,
  stripSourceComments,
} from "@shared/api/test-utils.js";

const ROOT = new URL("./", import.meta.url);

/** Paths relative to web/src. */
function walk(glob: string): string[] {
  return Array.from(
    new Bun.Glob(glob).scanSync(decodeURIComponent(ROOT.pathname)),
  ).sort();
}

async function read(path: string): Promise<string> {
  return Bun.file(new URL(path, ROOT)).text();
}

interface Guard {
  name: string;
  /** The files the guard's tree scan covers. */
  files: string;
  /** Findings for one file (empty = pass). */
  check: (path: string, text: string) => string[] | Promise<string[]>;
  bad: { path: string; text: string };
  clean: { path: string; text: string };
  /** Minimum files the effective scan must inspect. When a guard declares
      `scanned`, those paths are excluded before the floor is checked and
      the check runs — a guard that exempts inside `check` must mirror it
      here, or skipped files could satisfy the floor. */
  minScanned: number;
  /** Optional: paths excluded before the floor is checked and the check
      runs, mirroring the exclusions inside `check`. */
  scanned?: (path: string) => boolean;
}

const guardList: Guard[] = [];
function guard(spec: Guard): void {
  guardList.push(spec);
}

// ── Placement ─────────────────────────────────────────────────────────

guard({
  name: "route targets live at the feature root",
  files: "**/*.ts",
  check: (path) =>
    /(^|\/)ui\/[a-z-]*-page\.(ts|test\.ts)$/.test(path)
      ? [`${path}: a page module belongs at the feature root, not in ui/`]
      : [],
  bad: {
    path: "features/blog/ui/blog-list-page.ts",
    text: "",
  },
  clean: { path: "features/blog/blog-list-page.ts", text: "" },
  minScanned: 50,
});

guard({
  name: "every page module is registered at the feature root",
  files: "core/index.ts",
  check: async (_path, text) => {
    const findings: string[] = [];
    for (const match of text.matchAll(
      /(?:from\s+)?"(@features\/[a-z-]+\/ui\/[^"]+)"/g,
    )) {
      findings.push(`a page import still points into ui/: ${match[1]}`);
    }
    for (const match of text.matchAll(
      /"(@features\/([a-z-]+)\/([a-z-]+-page)\.js)"/g,
    )) {
      const module = `features/${match[2]}/${match[3]}.ts`;
      if (!(await Bun.file(new URL(module, ROOT)).exists())) {
        findings.push(`registered page module does not exist: ${module}`);
      }
    }
    return findings;
  },
  bad: {
    path: "core/index.ts",
    text: 'import "@features/auth/ui/sign-in-page.js";',
  },
  clean: {
    path: "core/index.ts",
    text: 'import "@features/auth/sign-in-page.js";',
  },
  minScanned: 1,
});

// ── Layer contracts ───────────────────────────────────────────────────

guard({
  name: "shared/** never imports features/**",
  files: "shared/**/*.ts",
  check: (path, text) => {
    const findings: string[] = [];
    if (!path.startsWith("shared/")) return findings;
    // Any string literal that reaches into a feature: the alias form, a
    // relative hop, and the dynamic-import call all count.
    if (/["'`][^"'`]*(?:@features\/|\.\.\/features\/)/.test(text)) {
      findings.push(`${path}: shared code imports a feature`);
    }
    return findings;
  },
  bad: {
    path: "shared/ui/thing.ts",
    text: 'import { el } from "@features/auth/data-access/session.js";',
  },
  clean: {
    path: "shared/ui/thing.ts",
    text: 'import { el } from "@shared/catalog/el.js";',
  },
  minScanned: 15,
});

// ── Transport boundary ────────────────────────────────────────────────

/** The only production files allowed a raw `fetch(`: the shared transport
    itself, and the footer's recorded `/health/ready` probe
    (core/shell/AGENTS.md). Aliased calls (`const f = fetch`) are outside
    the pattern; this list is the contract. */
const RAW_FETCH_EXEMPT = ["shared/api/client.ts", "core/shell/app-footer.ts"];

guard({
  name: "raw fetch lives only in the transport or the recorded probe",
  files: "**/*.ts",
  scanned: (path) =>
    !path.endsWith(".test.ts") && !RAW_FETCH_EXEMPT.includes(path),
  check: (path, text) => {
    if (path.endsWith(".test.ts") || RAW_FETCH_EXEMPT.includes(path)) return [];
    return /\bfetch\s*\(/.test(stripSourceComments(text))
      ? [`${path}: a raw fetch outside the transport`]
      : [];
  },
  bad: {
    path: "features/blog/blog-list-page.ts",
    text: 'const res = await fetch("/api/v1/blog-posts");',
  },
  clean: {
    path: "features/blog/blog-list-page.ts",
    text: 'const posts = await request<BlogPostList>("GET", "/blog-posts");',
  },
  minScanned: 80,
});

// ── Feature isolation ─────────────────────────────────────────────────

/** The recorded cross-feature edges (components.md rule 2). The session
    context is the one edge open to every feature; the rest are
    file-specific capability imports. A new edge means a row here and in
    the pattern doc. */
const UNIVERSAL_CROSS_FEATURE_IMPORTS = [
  "@features/auth/data-access/session.js",
];
const CROSS_FEATURE_EDGES: Array<{ from: string; specifier: string }> = [
  {
    from: "features/blog/blog-detail-page.ts",
    specifier: "@features/auth/ui/favorite-toggle.js",
  },
  {
    from: "features/blog/blog-detail-page.ts",
    specifier: "@features/auth/ui/follow-toggle.js",
  },
  {
    from: "features/blog/blog-detail-page.ts",
    specifier: "@features/comments/ui/comments-section.js",
  },
  {
    from: "features/projects/project-detail-page.ts",
    specifier: "@features/auth/ui/favorite-toggle.js",
  },
  {
    from: "features/projects/project-detail-page.ts",
    specifier: "@features/auth/ui/follow-toggle.js",
  },
  {
    from: "features/projects/project-detail-page.ts",
    specifier: "@features/comments/ui/comments-section.js",
  },
  {
    from: "features/auth/account-page.ts",
    specifier: "@features/notifications/ui/push-settings.js",
  },
];

/** Resolve a relative import specifier against the importing file's
    directory, so a `../<other>/…` hop is classified like the alias form. */
function resolveRelative(fromPath: string, specifier: string): string {
  const parts = fromPath.split("/").slice(0, -1);
  for (const segment of specifier.split("/")) {
    if (segment === "." || segment === "") continue;
    if (segment === "..") parts.pop();
    else parts.push(segment);
  }
  return parts.join("/");
}

/** The module path a specifier resolves to (`features/…`), or null for
    anything that does not point into a feature. The literal `@features/…`
    and `../…` spellings normalise to the same path, so a recorded edge
    passes in either form. Dot-segment tricks (`@features/../…`, `./../…`,
    climbs above the root) and template-literal specifiers are outside this
    lexical check. */
function crossFeatureTarget(
  fromPath: string,
  specifier: string,
): string | null {
  if (specifier.startsWith("@features/")) return specifier.slice(1);
  if (specifier.startsWith("../")) return resolveRelative(fromPath, specifier);
  return null;
}

/** Findings for one file: every import that reaches another feature must
    be a recorded edge. Test files may import fixtures freely. A computed or
    template-literal specifier is outside the scan — keep cross-feature
    specifiers literal. */
function crossFeatureFindings(path: string, text: string): string[] {
  if (!path.startsWith("features/") || path.endsWith(".test.ts")) return [];
  const own = path.split("/")[1] ?? "";
  const findings: string[] = [];
  const code = stripSourceComments(text);
  for (const match of code.matchAll(
    /(?:from\s+|import\s*\(?\s*)["']([^"']+)["']/g,
  )) {
    const specifier = match[1] ?? "";
    const target = crossFeatureTarget(path, specifier);
    if (target === null) continue;
    const targetFeature = target.match(/^features\/([a-z-]+)\//)?.[1];
    if (targetFeature === undefined || targetFeature === own) continue;
    const universal = UNIVERSAL_CROSS_FEATURE_IMPORTS.some(
      (entry) => crossFeatureTarget(path, entry) === target,
    );
    if (universal) continue;
    const recorded = CROSS_FEATURE_EDGES.some(
      (edge) =>
        edge.from === path &&
        crossFeatureTarget(path, edge.specifier) === target,
    );
    if (!recorded) {
      findings.push(`${path}: unrecorded cross-feature import ${specifier}`);
    }
  }
  return findings;
}

guard({
  name: "feature→feature imports stay in the recorded edge list",
  files: "features/**/*.ts",
  scanned: (path) => !path.endsWith(".test.ts"),
  check: crossFeatureFindings,
  bad: {
    path: "features/blog/blog-list-page.ts",
    text: 'import "@features/admin/admin-page.js";',
  },
  clean: {
    path: "features/blog/blog-list-page.ts",
    text: 'import "@features/auth/data-access/session.js";',
  },
  minScanned: 50,
});

// ── CSS hygiene ───────────────────────────────────────────────────────

/** The token home: the shell declares every --sf-* custom property. */
const TOKEN_HOME = "core/shell/app-shell.css";
/** The global document sheet cannot see the shell's custom properties
    (they are set on the shell host, inside the document), so its
    document-level focus baseline keeps its literal fallback. */
const HEX_EXEMPT = [TOKEN_HOME, "core/index.css"];

guard({
  name: "hex literals live only in app-shell.css",
  files: "**/*.css",
  check: (path, text) => {
    if (HEX_EXEMPT.includes(path)) return [];
    const findings: string[] = [];
    for (const match of stripCssComments(text).matchAll(
      /#[0-9a-fA-F]{3,8}\b/g,
    )) {
      findings.push(`${path}: hex literal ${match[0]}`);
    }
    return findings;
  },
  bad: { path: "features/x/x.css", text: ".a { color: #fff; }" },
  clean: {
    path: "features/x/x.css",
    text: ".a { color: var(--sf-on-accent); }",
  },
  minScanned: 25,
});

guard({
  name: "every --sf-* token used is declared in app-shell.css",
  files: "**/*.css",
  check: (path, text) => {
    const findings: string[] = [];
    for (const match of text.matchAll(/var\((--sf-[a-z-]+)/g)) {
      if (!declaredTokens.has(match[1] ?? "")) {
        findings.push(`${path}: undefined token ${match[1]}`);
      }
    }
    return findings;
  },
  bad: { path: "features/x/x.css", text: ".a { color: var(--sf-nope); }" },
  clean: { path: "features/x/x.css", text: ".a { color: var(--sf-text); }" },
  minScanned: 25,
});

guard({
  name: "no !important outside the reduced-motion overrides",
  files: "**/*.css",
  check: (path, text) => {
    // The reduced-motion override is the one legitimate use, and it lives in
    // exactly two sheets: the global document sheet and the in-shadow
    // baseline. Anywhere else, `!important` is a specificity failure.
    if (!["core/index.css", "core/styles/baseline.css"].includes(path)) {
      return stripCssComments(text).match(/!important/g)
        ? [`${path}: !important outside a reduced-motion override`]
        : [];
    }
    const stripped = text.replace(
      /@media \(prefers-reduced-motion: reduce\) \{[\s\S]*?\n\}/g,
      "",
    );
    return (stripCssComments(stripped).match(/!important/g) ?? []).map(
      () => `${path}: !important outside a reduced-motion override`,
    );
  },
  bad: {
    path: "features/x/x.css",
    text: ".a { color: red !important; }",
  },
  clean: {
    path: "core/styles/baseline.css",
    text: "@media (prefers-reduced-motion: reduce) {\n  * {\n    animation-duration: 0.01ms !important;\n  }\n}",
  },
  minScanned: 25,
});

/** The width ladder as accepted — the header keeps its own ladder and the
    content widths stay. A new width needs a recorded reason here. */
const BREAKPOINTS = new Set([
  "480px",
  "560px",
  "640px",
  "641px",
  "700px",
  "700.01px",
  "1150px",
  "1300px",
  "1920.01px",
]);

guard({
  name: "breakpoint allowlist",
  files: "**/*.css",
  check: (path, text) => {
    const findings: string[] = [];
    const stripped = stripCssComments(text);
    for (const media of stripped.matchAll(/@media([^{]*)\{/g)) {
      for (const width of (media[1] ?? "").matchAll(
        /(?:min|max)-width:\s*([\d.]+px)/g,
      )) {
        if (!BREAKPOINTS.has(width[1] ?? "")) {
          findings.push(`${path}: new breakpoint ${width[1]}`);
        }
      }
    }
    return findings;
  },
  bad: {
    path: "features/x/x.css",
    text: "@media (max-width: 999px) { .a { color: red; } }",
  },
  clean: {
    path: "features/x/x.css",
    text: "@media (max-width: 700px) { .a { color: red; } }",
  },
  minScanned: 25,
});

// ── Component sheets ──────────────────────────────────────────────────

guard({
  name: "every styled component imports both shared sheets",
  files: "**/*.ts",
  check: (path, text) => {
    if (!/static styles = \[/.test(text)) return [];
    const findings: string[] = [];
    // The import alone is not enough: the sheet must be in the styles array
    // (an unused import styles nothing).
    for (const [sheet, usage] of [
      ["baseline.css?inline", "unsafeCSS(sharedStyles)"],
      ["controls.css?inline", "unsafeCSS(controlStyles)"],
    ] as const) {
      if (!text.includes(sheet) || !text.includes(usage)) {
        findings.push(`${path}: missing the shared ${sheet} sheet`);
      }
    }
    return findings;
  },
  bad: {
    path: "features/x/x-page.ts",
    text: [
      'import sharedStyles from "@core/styles/baseline.css?inline";',
      "static styles = [unsafeCSS(styles)];",
    ].join("\n"),
  },
  clean: {
    path: "features/x/x-page.ts",
    text: [
      'import sharedStyles from "@core/styles/baseline.css?inline";',
      'import controlStyles from "@shared/ui/controls.css?inline";',
      "static styles = [unsafeCSS(sharedStyles), unsafeCSS(controlStyles)];",
    ].join("\n"),
  },
  minScanned: 30,
});

// ── Form semantics (the Enter-submission contract) ────────────────────

guard({
  name: "every form keeps a native submit button in its own tree",
  files: "**/*.ts",
  check: (path, text) => {
    const findings: string[] = [];
    // Test files carry template source as data (self-check snippets),
    // never a rendered form.
    if (path.endsWith(".test.ts")) return findings;
    for (const form of text.matchAll(/<form\b[\s\S]*?<\/form>/g)) {
      if (!/<(?:button|input)[^>]*type=['"]submit['"]/.test(form[0])) {
        findings.push(
          `${path}: a form has no native submit control in the same shadow root`,
        );
      }
    }
    return findings;
  },
  bad: {
    path: "features/x/x-page.ts",
    text: "<form><my-field></my-field></form>",
  },
  clean: {
    path: "features/x/x-page.ts",
    text: '<form><input /><button type="submit">Go</button></form>',
  },
  minScanned: 15,
});

// ── The card surface ──────────────────────────────────────────────────

/** The surfaces that render content cards; each must compose the shared
    element rather than hand-roll card markup. The admin users member list
    (`features/admin/admin-users-page.ts`) is the recorded exception
    (components.md rule 15): an interactive management surface, not a
    content listing. */
const CARD_SURFACES = [
  "features/blog/blog-list-page.ts",
  "features/blog/blog-detail-page.ts",
  "features/projects/projects-list-page.ts",
  "features/projects/project-detail-page.ts",
  "features/search/search-page.ts",
  "features/auth/ui/account-favorites-section.ts",
  "features/admin/ui/admin-list-base.ts",
];

guard({
  name: "content surfaces render cards through content-card",
  files: "**/*.ts",
  check: (path, text) => {
    if (!CARD_SURFACES.includes(path)) return [];
    // Comments may NAME the element (the module doc comments do); only the
    // rendered markup counts, so the check reads the code without them.
    const code = text
      .replace(/\/\*[\s\S]*?\*\//g, "")
      // Line comments only where they start a line: a mid-line `//` may be
      // inside a URL literal.
      .replace(/^\s*\/\/[^\n]*/gm, "");
    return code.includes("<content-card")
      ? []
      : [`${path}: a content surface renders no <content-card>`];
  },
  bad: {
    path: "features/blog/blog-list-page.ts",
    // A comment that names the element does not satisfy the guard: only the
    // rendered markup counts.
    text: '// the page renders <content-card>\nconst title = "Τίτλος";',
  },
  clean: {
    path: "features/blog/blog-list-page.ts",
    text: 'html`<content-card shape="grid"></content-card>`',
  },
  minScanned: 50,
});

/** The card classes the surfaces used before they composed the element;
    each lives in exactly one sheet now. */
const RETIRED_CARD_CLASSES = [
  "card",
  "card-link",
  "card-thumb",
  "card-thumb-empty",
  "card-body",
  "card-title",
  "card-subtitle",
  "card-description",
  "card-footer",
  "card-meta",
  "hero",
  "hero-link",
  "hero-thumb",
  "hero-body",
  "hero-title",
  "hero-badge",
  "hero-subtitle",
  "hero-description",
  "hero-footer",
  "hero-meta",
  "hero-topline",
  "result",
  "result-link",
  "result-thumb",
  "result-body",
  "result-title",
  "result-description",
  "result-topline",
  "type-badge",
  "fav-item",
  "fav-link",
  "fav-thumb",
  "fav-body",
  "fav-title",
  "fav-desc",
  "fav-topline",
  "admin-row",
  "admin-row-main",
  "admin-row-topline",
  "admin-row-title",
  "admin-row-meta",
  "admin-row-secondary",
  "admin-row-description",
  "admin-thumb",
  "status-badge",
  "detail-thumb",
  "detail-header",
  "detail-title-row",
  "detail-title",
  "detail-subtitle",
  "detail-description",
  "detail-meta",
  "detail-stats",
  "meta-line",
  "meta-label",
  "meta-byline",
  "meta-separator",
];

const CARD_SHEET = "shared/ui/content-card.css";

guard({
  name: "card styling lives only in the card's sheet",
  files: "**/*.css",
  check: (path, text) => {
    if (path === CARD_SHEET) return [];
    const stripped = stripCssComments(text);
    const findings: string[] = [];
    for (const className of RETIRED_CARD_CLASSES) {
      // The lookahead keeps `.admin-row` from matching `.admin-row-title`;
      // each retired name is matched as a whole class.
      if (new RegExp(`\\.${className}(?![\\w-])`).test(stripped)) {
        findings.push(`${path}: .${className} belongs to ${CARD_SHEET}`);
      }
    }
    return findings;
  },
  bad: {
    path: "features/x/x.css",
    text: ".hero-thumb { width: 100%; }",
  },
  clean: {
    path: "features/x/x.css",
    text: ".card-grid { display: grid; }",
  },
  minScanned: 25,
});

// ── Module size ───────────────────────────────────────────────────────

/** A non-test module's ceiling; `catalog/el.ts` is a data table, not code. */
const MAX_MODULE_LINES = 700;
const SIZE_EXEMPT = ["shared/catalog/el.ts"];

function countLines(text: string): number {
  // One trailing newline is not a line; interior blank lines count (they
  // are content the file chose to carry).
  return text.replace(/\n$/, "").split("\n").length;
}

guard({
  name: "production web modules stay at or under 700 lines",
  files: "**/*.ts",
  check: (path, text) => {
    if (path.endsWith(".test.ts") || SIZE_EXEMPT.includes(path)) return [];
    const lines = countLines(text);
    return lines > MAX_MODULE_LINES
      ? [`${path}: ${lines} lines (limit ${MAX_MODULE_LINES})`]
      : [];
  },
  bad: {
    path: "features/x/x-page.ts",
    text: "x\n".repeat(MAX_MODULE_LINES) + "x",
  },
  clean: {
    path: "features/x/x-page.ts",
    text: "x\n".repeat(MAX_MODULE_LINES - 1) + "x\n",
  },
  minScanned: 80,
});

// ── List semantics ─────────────────────────────────────────────────

/** Every class a `list-style: none` rule governs on a `<ul>` — the sweep's
    expected set (the BREAKPOINTS shape). A rename or a new governed class
    must be recorded here, and every entry must still be governed, so the
    guard cannot hollow out silently. */
const LIST_STYLE_CLASSES = new Set([
  "about-links",
  "admin-list",
  "card-grid",
  "downloads-list",
  "fav-results",
  "members-list",
  "notifications-list",
  "results",
  "sessions-list",
]);

/** A `list-style: none` class drops the list role in some engines, so every
    `<ul>` wearing one needs an explicit `role="list"` (the about-page
    precedent). Returns the governed class names, the list sites seen, and a
    finding per site missing the role. */
function listRoleFindings(
  cssSources: Array<{ path: string; text: string }>,
  tsSources: Array<{ path: string; text: string }>,
): { governed: Set<string>; sites: number; findings: string[] } {
  const governed = new Set<string>();
  for (const { text } of cssSources) {
    // Each class-selector rule with its body: the selector carries the
    // class list, the body the declarations that drop list semantics.
    for (const rule of stripCssComments(text).matchAll(
      /([^{}]*\.[a-z][^{}]*)\{([^}]*)\}/g,
    )) {
      if (!/list-style:\s*none/.test(rule[2] ?? "")) continue;
      for (const cls of (rule[1] ?? "").matchAll(/\.([a-z][a-z0-9-]*)/g)) {
        governed.add(cls[1] ?? "");
      }
    }
  }
  const findings: string[] = [];
  let sites = 0;
  for (const { path, text } of tsSources) {
    for (const tag of stripSourceComments(text).matchAll(/<ul\b[^>]*>/g)) {
      const classAttr = tag[0].match(/\bclass="([^"]*)"/);
      if (!classAttr) continue;
      const classes = (classAttr[1] ?? "").split(/\s+/);
      if (!classes.some((cls) => governed.has(cls))) continue;
      sites += 1;
      if (!/\brole="list"/.test(tag[0])) {
        findings.push(
          `${path}: <ul class="${classAttr[1] ?? ""}"> lacks role="list"`,
        );
      }
    }
  }
  return { governed, sites, findings };
}

// ── The declared token set ────────────────────────────────────────────

const declaredTokens = new Set(
  [
    ...stripCssComments(await read(TOKEN_HOME)).matchAll(
      /--sf-[a-z-]+(?=\s*:)/g,
    ),
  ].map((m) => m[0]),
);

// ── The run ───────────────────────────────────────────────────────────

describe("web architecture guards", () => {
  test("the token home declares the tokens the guards rely on", () => {
    expect(declaredTokens.size).toBeGreaterThan(20);
    for (const token of ["--sf-accent", "--sf-on-accent", "--sf-error"]) {
      expect(declaredTokens.has(token)).toBe(true);
    }
  });

  test("every card surface exists where the card guard scans", async () => {
    // The card-surface guard can only demand the element from a path that
    // is still spelled the same; a rename must land here too, or the
    // surface would silently drop out of the guard.
    expect(CARD_SURFACES.length).toBeGreaterThanOrEqual(7);
    for (const path of CARD_SURFACES) {
      expect(await Bun.file(new URL(path, ROOT)).exists()).toBe(true);
    }
  });

  test("every page module is imported by the app (registered)", async () => {
    // Either core/index.ts or another registered component's module graph
    const modules = walk("**/*.ts").filter((p) => !p.endsWith(".test.ts"));
    const all = (await Promise.all(modules.map((path) => read(path)))).join(
      "\n",
    );
    const pages = walk("features/**/*-page.ts");
    expect(pages.length).toBeGreaterThan(15);
    for (const path of pages) {
      const specifier = `@${path.replace(/-page\.ts$/, "-page.js")}`;
      expect(all).toContain(`"${specifier}"`);
    }
  });

  test("the cross-feature classifier covers alias, relative, and dynamic forms", () => {
    expect(
      crossFeatureFindings(
        "features/blog/blog-list-page.ts",
        'import "@features/admin/admin-page.js";',
      ),
    ).toHaveLength(1);
    expect(
      crossFeatureFindings(
        "features/blog/blog-list-page.ts",
        'await import("@features/admin/admin-page.js");',
      ),
    ).toHaveLength(1);
    expect(
      crossFeatureFindings(
        "features/blog/blog-list-page.ts",
        'import "../admin/admin-page.js";',
      ),
    ).toHaveLength(1);
    // A recorded edge passes in either spelling; the universal session edge
    // and same-feature hops never count.
    expect(
      crossFeatureFindings(
        "features/blog/blog-detail-page.ts",
        'import "../auth/ui/favorite-toggle.js";',
      ),
    ).toEqual([]);
    expect(
      crossFeatureFindings(
        "features/blog/blog-list-page.ts",
        'import "@features/auth/data-access/session.js";',
      ),
    ).toEqual([]);
    expect(
      crossFeatureFindings(
        "features/blog/blog-list-page.ts",
        'import "./ui/thing.js";',
      ),
    ).toEqual([]);
  });

  test("list-style:none lists carry an explicit role=list", async () => {
    // Self-check: a governed class fails without the role, the role clears
    // it, and a commented-out rule governs nothing.
    expect(
      listRoleFindings(
        [{ path: "features/x/x.css", text: ".x { list-style: none; }" }],
        [{ path: "features/x/x.ts", text: '<ul class="x"></ul>' }],
      ).findings,
    ).toHaveLength(1);
    expect(
      listRoleFindings(
        [{ path: "features/x/x.css", text: ".x { list-style: none; }" }],
        [{ path: "features/x/x.ts", text: '<ul class="x" role="list"></ul>' }],
      ).findings,
    ).toEqual([]);
    expect(
      listRoleFindings(
        [{ path: "features/x/x.css", text: "/* .x { list-style: none; } */" }],
        [{ path: "features/x/x.ts", text: '<ul class="x"></ul>' }],
      ).findings,
    ).toEqual([]);
    // A media-nested rule still governs (the scan reads the flat body).
    expect(
      listRoleFindings(
        [
          {
            path: "features/x/x.css",
            text: "@media (max-width: 700px) { .x { list-style: none; } }",
          },
        ],
        [{ path: "features/x/x.ts", text: '<ul class="x"></ul>' }],
      ).findings,
    ).toHaveLength(1);
    // A comma-separated selector governs every class it names.
    expect(
      listRoleFindings(
        [{ path: "features/x/x.css", text: ".x, .y { list-style: none; }" }],
        [
          { path: "features/x/x.ts", text: '<ul class="x"></ul>' },
          { path: "features/x/x.ts", text: '<ul class="y"></ul>' },
        ],
      ).findings,
    ).toHaveLength(2);
    // A `<ul>` inside a source comment is not a site.
    expect(
      listRoleFindings(
        [{ path: "features/x/x.css", text: ".x { list-style: none; }" }],
        [{ path: "features/x/x.ts", text: '// <ul class="x"></ul>' }],
      ).sites,
    ).toBe(0);

    const cssSources = await Promise.all(
      walk("**/*.css").map(async (path) => ({ path, text: await read(path) })),
    );
    const tsSources = await Promise.all(
      walk("**/*.ts")
        .filter((path) => !path.endsWith(".test.ts"))
        .map(async (path) => ({ path, text: await read(path) })),
    );
    const { governed, sites, findings } = listRoleFindings(
      cssSources,
      tsSources,
    );
    // The allowlist and the governed set match in both directions: a
    // governed class missing above and an allowlisted class no longer
    // governed both fail here, so a rename cannot hide.
    expect([...governed].sort()).toEqual([...LIST_STYLE_CLASSES].sort());
    // Non-vacuity: a rename must not silently hollow the sweep out.
    expect(governed.size).toBeGreaterThanOrEqual(8);
    expect(sites).toBeGreaterThanOrEqual(8);
    expect(findings).toEqual([]);
  });

  for (const spec of guardList) {
    describe(spec.name, () => {
      test("self-check: the bad snippet fails and the clean one passes", async () => {
        expect(
          (await spec.check(spec.bad.path, spec.bad.text)).length,
        ).toBeGreaterThan(0);
        expect(await spec.check(spec.clean.path, spec.clean.text)).toEqual([]);
      });

      test("the tree scan is clean and non-vacuous", async () => {
        const files = walk(spec.files).filter(spec.scanned ?? (() => true));
        expect(files.length).toBeGreaterThanOrEqual(spec.minScanned);
        const findings: string[] = [];
        for (const path of files) {
          const text = await read(path);
          findings.push(...(await spec.check(path, text)));
        }
        expect(findings).toEqual([]);
      });
    });
  }
});
