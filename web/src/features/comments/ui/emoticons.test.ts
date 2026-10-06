/**
 * Emoticon rendering tests — the token set, the
 * longest-match/case-insensitive/URL-skipping tokenizer,
 * and the colored SVG glyphs (label + palette + the shared heart geometry).
 */

import { describe, expect, test } from "bun:test";
import { html, render } from "lit";

import { el } from "@shared/catalog/el.js";
import { heartPath } from "@shared/ui/icons.js";

import {
  commentBodyTemplate,
  emoticonLabel,
  tokenizeBody,
  type EmoticonName,
} from "./emoticons.js";

/** Render a body the way the thread does and return the host element. */
function renderBody(body: string): HTMLElement {
  const host = document.createElement("p");
  render(html`${commentBodyTemplate(body)}`, host);
  return host;
}

const ALL_TOKENS: ReadonlyArray<[string, EmoticonName]> = [
  [":)", "smile"],
  [":-)", "smile"],
  ["=)", "smile"],
  ["=-)", "smile"],
  [":D", "grin"],
  [":-D", "grin"],
  [":(", "frown"],
  [":-(", "frown"],
  [";)", "wink"],
  [";-)", "wink"],
  [":P", "tongue"],
  [":-P", "tongue"],
  ["<3", "heart"],
  [":O", "surprised"],
  [":-O", "surprised"],
  [":/", "unsure"],
  [":-/", "unsure"],
];

describe("emoticon tokenizer", () => {
  test("plain text without tokens stays one text segment", () => {
    expect(tokenizeBody("Καλησπέρα σε όλους")).toEqual([
      { kind: "text", value: "Καλησπέρα σε όλους" },
    ]);
  });

  test("every ruled token maps to its glyph", () => {
    for (const [token, name] of ALL_TOKENS) {
      expect(tokenizeBody(token)).toEqual([{ kind: "emoticon", name }]);
    }
  });

  test("tokens are matched longest first", () => {
    // `:-)` must not render as `:` plus `-)` (or as `:)` plus `-`).
    expect(tokenizeBody(":-)")).toEqual([{ kind: "emoticon", name: "smile" }]);
    expect(tokenizeBody(":-P")).toEqual([{ kind: "emoticon", name: "tongue" }]);
  });

  test("matching is case-insensitive", () => {
    expect(tokenizeBody(":p")).toEqual([{ kind: "emoticon", name: "tongue" }]);
    expect(tokenizeBody(":d")).toEqual([{ kind: "emoticon", name: "grin" }]);
    expect(tokenizeBody(":o")).toEqual([
      { kind: "emoticon", name: "surprised" },
    ]);
  });

  test("keeps the surrounding text and line breaks exactly", () => {
    expect(tokenizeBody("α\n:) β")).toEqual([
      { kind: "text", value: "α\n" },
      { kind: "emoticon", name: "smile" },
      { kind: "text", value: " β" },
    ]);
  });

  test("a token inside a URL is left as text", () => {
    expect(tokenizeBody("https://example.com/:( σελίδα")).toEqual([
      { kind: "text", value: "https://example.com/:( σελίδα" },
    ]);
    expect(tokenizeBody("www.example.com/<3")).toEqual([
      { kind: "text", value: "www.example.com/<3" },
    ]);
  });

  test("near-misses stay text", () => {
    for (const body of ["8:30", ":", "(", "a:b", "< 3", ";("]) {
      expect(tokenizeBody(body)).toEqual([{ kind: "text", value: body }]);
    }
  });

  test("an empty body has no segments", () => {
    expect(tokenizeBody("")).toEqual([]);
  });

  test("a mixed body splits into the expected segments, in order", () => {
    // The property the module documents — nothing is dropped and nothing is
    // re-ordered — pinned on one body that carries text, tokens and a URL.
    expect(tokenizeBody("γεια :) :-D https://x.test/:( <3 ;)")).toEqual([
      { kind: "text", value: "γεια " },
      { kind: "emoticon", name: "smile" },
      { kind: "text", value: " " },
      { kind: "emoticon", name: "grin" },
      { kind: "text", value: " https://x.test/:( " },
      { kind: "emoticon", name: "heart" },
      { kind: "text", value: " " },
      { kind: "emoticon", name: "wink" },
    ]);
  });
});

describe("emoticon glyphs", () => {
  test("a token renders a colored, labelled svg and the text around it survives", () => {
    const host = renderBody("γεια :) φίλε");

    const svg = host.querySelector("svg.ct-emoticon");
    expect(svg).not.toBeNull();
    expect(svg!.getAttribute("role")).toBe("img");
    expect(svg!.getAttribute("aria-label")).toBe(el.comments.emoticonSmile);
    // The classic emoji palette — the face is the ruled yellow, the features
    // the ruled brown (colored on every platform, no font dependency).
    expect(svg!.querySelector('circle[fill="#ffcc4d"]')).not.toBeNull();
    expect(svg!.querySelector('circle[fill="#664500"]')).not.toBeNull();
    // The ASCII token itself is gone from the rendered text; the words are
    // not, and the glyph adds no text of its own (whitespace-insensitive, so
    // an IDE reformat of the template cannot fail this).
    expect(host.textContent?.replace(/\s+/g, " ")).toBe("γεια φίλε");
    expect(svg!.textContent?.trim()).toBe("");
  });

  test("every glyph renders with its own Greek label and the face palette", () => {
    const names: EmoticonName[] = [
      "smile",
      "grin",
      "frown",
      "wink",
      "tongue",
      "heart",
      "surprised",
      "unsure",
    ];
    for (const name of names) {
      const host = renderBody(tokenFor(name));
      const svg = host.querySelector("svg.ct-emoticon");
      expect(svg).not.toBeNull();
      expect(svg!.getAttribute("aria-label")).toBe(emoticonLabel(name));
      expect(svg!.getAttribute("aria-label")).toBeTruthy();
      if (name !== "heart") {
        expect(svg!.querySelector('circle[fill="#ffcc4d"]')).not.toBeNull();
      }
    }
  });

  test("the heart glyph fills the shared icon geometry (no divergent path)", () => {
    const host = renderBody("<3");
    const path = host.querySelector("path");

    expect(path?.getAttribute("d")).toBe(heartPath);
    expect(path?.getAttribute("fill")).toBe("#dd2e44");
  });

  test("an empty body renders nothing", () => {
    expect(renderBody("").querySelector("svg")).toBeNull();
  });
});

/** The first token of a name — for the per-glyph render pins. */
function tokenFor(name: EmoticonName): string {
  const found = ALL_TOKENS.find(([, n]) => n === name);
  if (!found) throw new Error(`no token for ${name}`);
  return found[0];
}
