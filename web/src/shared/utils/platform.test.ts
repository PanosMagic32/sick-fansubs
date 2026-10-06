import { afterEach, describe, expect, test } from "bun:test";

import { stubProperty } from "@shared/api/test-utils.js";

import { isIOS, isStandalone } from "./platform.js";

const UA_IPHONE =
  "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15";
const UA_MAC =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15";

const restores: Array<() => void> = [];

/** Shadow-stub with an immediate restore registration (afterEach). */
function stub(target: object, name: string, value: unknown): void {
  restores.push(stubProperty(target, name, value));
}

afterEach(() => {
  while (restores.length > 0) restores.pop()!();
});

describe("isIOS", () => {
  test("recognizes iPhone/iPad/iPod user agents", () => {
    stub(navigator, "userAgent", UA_IPHONE);
    expect(isIOS()).toBe(true);
  });

  test("treats iPadOS desktop-mode Safari (MacIntel + touch points) as iOS", () => {
    stub(navigator, "userAgent", UA_MAC);
    stub(navigator, "maxTouchPoints", 5);
    expect(isIOS()).toBe(true);
  });

  test("does not treat a real Mac as iOS", () => {
    stub(navigator, "userAgent", UA_MAC);
    stub(navigator, "maxTouchPoints", 0);
    expect(isIOS()).toBe(false);
  });
});

describe("isStandalone", () => {
  test("uses the iOS standalone flag", () => {
    stub(navigator, "standalone", true);
    expect(isStandalone()).toBe(true);
  });

  test("uses the display-mode media query (Android/desktop installs)", () => {
    stub(window, "matchMedia", (query: string) => ({
      matches: query === "(display-mode: standalone)",
      media: query,
    }));
    expect(isStandalone()).toBe(true);
  });

  test("is false in a plain browser tab", () => {
    expect(isStandalone()).toBe(false);
  });
});
