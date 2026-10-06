import { describe, expect, test } from "bun:test";

import { stubDebounceTimers } from "@shared/api/test-utils.js";

import { ToggleDebouncer } from "./toggle-debouncer.js";

describe("ToggleDebouncer", () => {
  test("delivers the last fire per key after the window expires", () => {
    const timers = stubDebounceTimers();
    try {
      const debouncer = new ToggleDebouncer();
      const fired: string[] = [];

      debouncer.schedule("a", () => fired.push("a1"));
      debouncer.schedule("a", () => fired.push("a2"));
      debouncer.schedule("a", () => fired.push("a3"));
      expect(timers.open()).toBe(1);

      timers.fireAll();
      expect(fired).toEqual(["a3"]);
    } finally {
      timers.restore();
    }
  });

  test("distinct keys debounce independently", () => {
    const timers = stubDebounceTimers();
    try {
      const debouncer = new ToggleDebouncer();
      const fired: string[] = [];

      debouncer.schedule("a", () => fired.push("a"));
      debouncer.schedule("b", () => fired.push("b"));
      expect(timers.open()).toBe(2);

      timers.fireAll();
      expect(fired.sort()).toEqual(["a", "b"]);
    } finally {
      timers.restore();
    }
  });

  test("cancel clears every window and every pending delivery", () => {
    const timers = stubDebounceTimers();
    try {
      const debouncer = new ToggleDebouncer();
      const fired: string[] = [];

      debouncer.schedule("a", () => fired.push("a"));
      debouncer.schedule("b", () => fired.push("b"));
      debouncer.cancel();
      expect(timers.open()).toBe(0);

      timers.fireAll();
      expect(fired).toEqual([]);
    } finally {
      timers.restore();
    }
  });
});
