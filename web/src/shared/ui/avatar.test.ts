/**
 * avatarTemplate pins the fallback chain: image → initial → empty circle.
 */

import { describe, expect, test } from "bun:test";

import { render } from "lit";

import { avatarTemplate } from "./avatar.js";

function avatarHTML(ref: Parameters<typeof avatarTemplate>[0]) {
  const container = document.createElement("div");
  render(avatarTemplate(ref), container);
  return container;
}

describe("avatarTemplate", () => {
  test("an avatarUrl renders a decorative image", () => {
    const root = avatarHTML({
      username: "katakuri",
      avatarUrl: "/media/aa/bb.png",
    });

    const img = root.querySelector("img.avatar");
    expect(img?.getAttribute("src")).toBe("/media/aa/bb.png");
    expect(img?.getAttribute("alt")).toBe("");
  });

  test("a username without an avatar renders the initial circle", () => {
    const root = avatarHTML({ username: "Ζορό", avatarUrl: null });

    const initial = root.querySelector("span.avatar.avatar-initial");
    expect(initial?.textContent).toBe("Ζ");
    expect(initial?.getAttribute("aria-hidden")).toBe("true");
  });

  test("no user renders the empty circle", () => {
    const root = avatarHTML(null);

    expect(root.querySelector("img")).toBeNull();
    expect(root.querySelector("span.avatar.avatar-empty")).not.toBeNull();
  });

  test("an empty username also falls back to the empty circle", () => {
    const root = avatarHTML({ username: "", avatarUrl: null });

    expect(root.querySelector("span.avatar.avatar-empty")).not.toBeNull();
  });
});
