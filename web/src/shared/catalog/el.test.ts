import { describe, expect, test } from "bun:test";

import {
  commentCountLabel,
  el,
  favoriteCountLabel,
  pagerPageOf,
  problemMessage,
  retryAfterMessage,
  unreadBadgeLabel,
  violationMessage,
} from "./el";

/**
 * Assert that every value in a catalog group is a non-empty string.
 * Enumeration over Object.entries means adding a key without a Greek
 * message (or with an empty one) fails here instead of drifting silently
 * in an unused branch.
 */
function expectAllNonEmpty(group: Record<string, unknown>, groupName: string) {
  for (const [key, value] of Object.entries(group)) {
    const label = `${groupName}.${key}`;
    // A nested map is a group of its own (el.admin.auditEventNames is the
    // first one): recurse rather than making the caller list it.
    if (typeof value === "object" && value !== null) {
      expectAllNonEmpty(value as Record<string, unknown>, label);
      continue;
    }
    expect(value, label).toBeString();
    expect((value as string).length, label).toBeGreaterThan(0);
  }
}

describe("el catalog", () => {
  test("every group is enumerated and non-empty", () => {
    expectAllNonEmpty(el.ui, "el.ui");
    expectAllNonEmpty(el.nav, "el.nav");
    expectAllNonEmpty(el.auth, "el.auth");
    expectAllNonEmpty(el.account, "el.account");
    expectAllNonEmpty(el.favorites, "el.favorites");
    expectAllNonEmpty(el.follows, "el.follows");
    expectAllNonEmpty(el.blog, "el.blog");
    expectAllNonEmpty(el.about, "el.about");
    expectAllNonEmpty(el.donation, "el.donation");
    expectAllNonEmpty(el.install, "el.install");
    expectAllNonEmpty(el.offline, "el.offline");
    expectAllNonEmpty(el.projects, "el.projects");
    expectAllNonEmpty(el.search, "el.search");
    expectAllNonEmpty(el.admin, "el.admin");
    expectAllNonEmpty(el.problems, "el.problems");
    expectAllNonEmpty(el.violations, "el.violations");
    expectAllNonEmpty(el.titles, "el.titles");
    expectAllNonEmpty(el.notifications, "el.notifications");
    expectAllNonEmpty(el.comments, "el.comments");
    expectAllNonEmpty(el.meta, "el.meta");
  });

  test("spot-checked copy stays stable", () => {
    // A handful of exact-value pins: these strings also appear in
    // component contract tests, so wording changes here ripple there.
    expect(el.ui.siteName).toBe("Sick-Fansubs");
    expect(el.ui.error).toBe("Κάτι πήγε στραβά.");
    expect(el.violations.required).toBe("Απαιτείται.");
    // The enumerated-value code the API answers for an unknown enum in a
    // query parameter or a request body.
    expect(el.violations.invalidValue).toBe("Μη έγκυρη τιμή.");
    expect(el.projects.projectsNotFound).toBe("Το project δε βρέθηκε.");
    // The unread feed row's sr-only state word — exact, since it rides an
    // accessible name.
    expect(el.notifications.unreadRow).toBe("Αδιάβαστη");
    // Exact value on purpose: a reword is a product decision (the copy must
    // never promise that cached pages load), so it is pinned here rather
    // than compared against the catalog in the component test.
    expect(el.offline.message).toBe(
      "Είσαι εκτός σύνδεσης. Κάποιες σελίδες μπορεί να μην φορτώσουν.",
    );
    expect(el.problems["/problems/auth/invalid-credentials"]).toBe(
      "Λάθος στοιχεία σύνδεσης.",
    );
    // Greek singular/plural split for the 429 countdown.
    expect(retryAfterMessage(1)).toBe("Δοκιμάστε ξανά σε 1 δευτερόλεπτο.");
    expect(retryAfterMessage(30)).toBe("Δοκιμάστε ξανά σε 30 δευτερόλεπτα.");
    // Register rule messages: the wording is a product decision — a reword
    // must fail here rather than drift silently.
    expect(el.auth.registerUsernameFormat).toBe(
      "Το όνομα χρήστη δέχεται λατινικά γράμματα, αριθμούς και κενά ανάμεσά τους — χωρίς άλλα σύμβολα.",
    );
    expect(el.auth.registerPasswordMin).toBe(
      "Ο κωδικός πρέπει να έχει τουλάχιστον 8 χαρακτήρες.",
    );
    // The donation label: the generic word replaces the platform brand.
    expect(el.about.donateLabel).toBe("Donate");
    // The about page's active-members roster is product data — a change is an
    // owner decision, so the exact list is pinned here.
    expect(el.about.members).toBe(
      "Kushoyarou, Katakuri, Medusa, Feitan, DarkClaw86, Akagami no Shanks.",
    );
    // The logs tables' time header is English, and the audit table's
    // technical headers follow.
    expect(el.admin.logsTimeColumn).toBe("timestamp");
    expect(el.admin.auditTimeColumn).toBe("timestamp");
    expect(el.admin.auditActorColumn).toBe("Actor (ID)");
    expect(el.admin.auditTargetColumn).toBe("Target (ID)");
    expect(el.admin.auditRequestColumn).toBe("Request (ID)");
    expect(el.admin.auditAddrColumn).toBe("Address");
    // The remaining register rule messages: same product-decision pin.
    expect(el.auth.registerUsernameLength).toBe(
      "Το όνομα χρήστη πρέπει να έχει 1–32 χαρακτήρες.",
    );
    expect(el.auth.registerUsernameTaken).toBe(
      "Το όνομα χρήστη χρησιμοποιείται ήδη.",
    );
    expect(el.auth.registerEmailFormat).toBe("Μη έγκυρη διεύθυνση email.");
    expect(el.auth.registerEmailTaken).toBe("Το email χρησιμοποιείται ήδη.");
    expect(el.auth.registerPasswordMax).toBe(
      "Ο κωδικός δεν μπορεί να υπερβαίνει τα 72 byte.",
    );
  });

  test("problemMessage returns Greek for known types", () => {
    expect(problemMessage("/problems/forbidden")).toBe("Δεν έχετε πρόσβαση.");
  });

  test("problemMessage falls back to the generic Greek error for unknown types", () => {
    // Raw machine paths must never reach the UI.
    expect(problemMessage("/problems/unknown-type")).toBe(el.ui.error);
  });

  test("violationMessage returns Greek for known codes", () => {
    expect(violationMessage("required")).toBe("Απαιτείται.");
  });

  test("violationMessage falls back to the generic Greek error for unknown codes", () => {
    // Raw codes must never reach the UI.
    expect(violationMessage("unknown-code")).toBe(el.ui.error);
  });

  test("unreadBadgeLabel keeps the Greek singular/plural split", () => {
    expect(unreadBadgeLabel(1)).toBe("1 αδιάβαστη ειδοποίηση");
    expect(unreadBadgeLabel(3)).toBe("3 αδιάβαστες ειδοποιήσεις");
  });

  test("pagerPageOf states the ordinal and the page count", () => {
    expect(pagerPageOf(1, 1)).toBe("Σελίδα 1 από 1");
    expect(pagerPageOf(2, 5)).toBe("Σελίδα 2 από 5");
    expect(pagerPageOf(12, 100)).toBe("Σελίδα 12 από 100");
  });

  test("indicator count labels keep the Greek singular/plural split", () => {
    expect(commentCountLabel(1)).toBe("1 σχόλιο");
    expect(commentCountLabel(5)).toBe("5 σχόλια");
    expect(commentCountLabel(0)).toBe("0 σχόλια");
    expect(favoriteCountLabel(1)).toBe("1 αγαπημένο");
    expect(favoriteCountLabel(2)).toBe("2 αγαπημένα");
  });
});
