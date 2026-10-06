/**
 * form-field directive tests — pin the rendered attribute contract.
 *
 * The page suites assert ids/aria wiring indirectly; this suite pins the
 * attribute-level contract in one place (type, maxlength, autocomplete,
 * required, aria-invalid/aria-describedby, error paragraph id) so a silent
 * directive regression can't slip through.
 */

import { describe, expect, test } from "bun:test";
import { render } from "lit";

import { formField } from "./form-field.js";

function fieldHTML(overrides: Partial<Parameters<typeof formField>[0]> = {}) {
  const container = document.createElement("div");
  render(
    formField({
      id: "f-id",
      label: "Ετικέτα",
      type: "text",
      value: "value",
      pending: false,
      onInput: () => {},
      ...overrides,
    }),
    container,
  );
  return container;
}

describe("form-field directive", () => {
  test("renders label, input, and attribute contract", () => {
    const root = fieldHTML({
      autocomplete: "username",
      autocapitalize: "none",
      spellcheck: false,
      maxlength: 32,
    });

    const label = root.querySelector("label");
    expect(label?.getAttribute("for")).toBe("f-id");
    expect(label?.textContent?.trim()).toBe("Ετικέτα");

    const input = root.querySelector("input");
    expect(input?.id).toBe("f-id");
    expect(input?.getAttribute("type")).toBe("text");
    expect(input?.getAttribute("autocomplete")).toBe("username");
    expect(input?.getAttribute("autocapitalize")).toBe("none");
    expect(input?.getAttribute("spellcheck")).toBe("false");
    expect(input?.getAttribute("maxlength")).toBe("32");
    expect(input?.hasAttribute("required")).toBe(true);
    expect(input?.value).toBe("value");
    expect(input?.hasAttribute("aria-invalid")).toBe(false);
    expect(input?.hasAttribute("aria-describedby")).toBe(false);
    expect(root.querySelector(".field-error")).toBeNull();
  });

  test("shows the error paragraph wired via aria-describedby", () => {
    const root = fieldHTML({ error: "Απαιτείται." });

    const input = root.querySelector("input");
    expect(input?.getAttribute("aria-invalid")).toBe("true");
    expect(input?.getAttribute("aria-describedby")).toBe("f-id-error");

    const error = root.querySelector("#f-id-error");
    expect(error?.classList.contains("field-error")).toBe(true);
    expect(error?.textContent?.trim()).toBe("Απαιτείται.");
  });

  test("renders an optional hint wired via aria-describedby", () => {
    const root = fieldHTML({ hint: "Οδηγία πεδίου." });

    const hint = root.querySelector("#f-id-hint");
    expect(hint?.classList.contains("field-hint")).toBe(true);
    expect(hint?.textContent?.trim()).toBe("Οδηγία πεδίου.");
    expect(root.querySelector("input")?.getAttribute("aria-describedby")).toBe(
      "f-id-hint",
    );
  });

  test("combines error and hint ids in aria-describedby", () => {
    const root = fieldHTML({ error: "Απαιτείται.", hint: "Οδηγία πεδίου." });
    expect(root.querySelector("input")?.getAttribute("aria-describedby")).toBe(
      "f-id-error f-id-hint",
    );
  });

  test("disables the input while pending", () => {
    const root = fieldHTML({ pending: true });
    expect(root.querySelector("input")?.hasAttribute("disabled")).toBe(true);
  });

  test("forwards input events with the new value", () => {
    const values: string[] = [];
    const root = fieldHTML({ onInput: (v) => values.push(v) });

    const input = root.querySelector("input") as HTMLInputElement;
    input.value = "typed";
    input.dispatchEvent(new InputEvent("input"));

    expect(values).toEqual(["typed"]);
  });

  test("omits autocomplete, autocapitalize, spellcheck, and maxlength when undefined", () => {
    const root = fieldHTML();
    const input = root.querySelector("input");
    expect(input?.hasAttribute("autocomplete")).toBe(false);
    expect(input?.hasAttribute("autocapitalize")).toBe(false);
    expect(input?.hasAttribute("spellcheck")).toBe(false);
    expect(input?.hasAttribute("maxlength")).toBe(false);
  });
});
