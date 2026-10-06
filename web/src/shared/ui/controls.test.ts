import { describe, expect, test } from "bun:test";

import { stripCssComments } from "@shared/api/test-utils.js";

import { BUTTON_VARIANTS, buttonClass } from "./controls.js";

const controlsCss = stripCssComments(
  await Bun.file(new URL("./controls.css", import.meta.url)).text(),
);

describe("controls", () => {
  test("buttonClass renders the base class alone by default", () => {
    expect(buttonClass()).toBe("button");
  });

  test("buttonClass maps each option to its class", () => {
    expect(buttonClass({ variant: "primary" })).toBe("button button--primary");
    expect(buttonClass({ variant: "danger", sm: true })).toBe(
      "button button--danger button--sm",
    );
    expect(buttonClass({ icon: true, variant: "ghost" })).toBe(
      "button button--ghost button--icon",
    );
    expect(buttonClass({ extra: "member-save" })).toBe("button member-save");
    expect(buttonClass({ variant: "link", icon: true, sm: true })).toBe(
      "button button--link button--icon button--sm",
    );
  });

  test("every variant has a declared modifier in controls.css", () => {
    for (const variant of BUTTON_VARIANTS) {
      expect(controlsCss).toContain(`.button--${variant} {`);
    }
  });

  test("the modifier list is closed", () => {
    expect([...BUTTON_VARIANTS]).toEqual([
      "primary",
      "secondary",
      "danger",
      "ghost",
      "link",
    ]);
  });

  test("the icon modifier keeps a custom-minimum target floor", () => {
    expect(controlsCss).toMatch(
      /\.button--icon\s*\{[^}]*min-width:\s*2rem;[^}]*min-height:\s*2rem;/,
    );
    expect(controlsCss).toMatch(
      /\.button--sm\.button--icon\s*\{[^}]*min-width:\s*2rem;[^}]*min-height:\s*2rem;/,
    );
  });
});
