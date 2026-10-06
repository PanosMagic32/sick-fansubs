/**
 * Admin logs tab tests — the
 * super-admin floor (narrower than the dashboard's moderator+), the two
 * INDEPENDENT sections (log tail + audit browser) with their own filters,
 * states, and retries, the URL-primed views, and the empty/error cases.
 */

import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";

import { el } from "@shared/catalog/el.js";
import { formatDateTimeNumeric } from "@shared/utils/format.js";
import {
  mockFetchImpl,
  mockResponse,
  mockSessionContext,
  resetHappyDOMURL,
  restoreHistoryLocationSync,
  restoreMockFetch,
  setHappyDOMURL,
  shimHistoryLocationSync,
  stripCssComments,
} from "@shared/api/test-utils.js";

import "@features/auth/data-access/session.js";
import "./admin-logs-page.js";
import type { AdminLogsPage } from "./admin-logs-page.js";

// The stylesheet is read from disk (the search-page precedent): the status
// chips' token palette is pinned against the source with its comments
// stripped, because happy-dom resolves no custom properties.
const logsStyles = stripCssComments(
  await Bun.file(new URL("./admin-logs-page.css", import.meta.url)).text(),
);

/** sf-navigate listeners registered by a test — removed in afterEach (a
 * leaked window listener outlives the shared happy-dom worker). */
const navCleanups: Array<() => void> = [];

function mockSession(
  status: "authenticated" | "anonymous" | "error" = "authenticated",
  role = "super-admin",
) {
  return mockSessionContext({ status, user: { username: "Super", role } });
}

type FetchResult = { status: number; body: unknown };

/** Stubs fetch and records every requested URL. */
function stubFetch(handler: (url: string) => Promise<FetchResult>) {
  const urls: string[] = [];
  mockFetchImpl(async (input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    urls.push(url);
    const res = await handler(url);
    return mockResponse({
      ok: res.status < 400,
      status: res.status,
      headers:
        res.status >= 400 ? { "Content-Type": "application/problem+json" } : {},
      jsonBody: res.body,
    });
  });
  return urls;
}

/**
 * Stubs the two reads the page makes, dispatching on the path so a test can
 * fail one section without touching the other. Both default to a healthy body.
 */
function stubStaffReads(
  logs: (url: string) => Promise<FetchResult> = async () => ({
    status: 200,
    body: LOGS_BODY,
  }),
  audit: (url: string) => Promise<FetchResult> = async () => ({
    status: 200,
    body: AUDIT_BODY,
  }),
) {
  return stubFetch((url) =>
    url.includes("/staff/audit-events") ? audit(url) : logs(url),
  );
}

const logsOf = (urls: string[]) =>
  urls.filter((u) => u.includes("/staff/logs"));
const auditsOf = (urls: string[]) =>
  urls.filter((u) => u.includes("/staff/audit-events"));

const LOGS_BODY = {
  items: [
    {
      time: "2026-09-18T10:00:00.000Z",
      level: "error",
      msg: "staff metrics totals failed",
      fields: { error: "boom", requestID: "r1" },
    },
    {
      time: "2026-09-18T09:00:00.000Z",
      level: "warn",
      msg: "rate limit exceeded",
      fields: {},
    },
  ],
};

const AUDIT_BODY = {
  items: [
    {
      id: "e2",
      event: "role_changed",
      result: "success",
      actorId: "actor-1",
      targetId: "target-1",
      targetRole: "moderator",
      requestId: "req-2",
      remoteAddr: "192.0.2.1",
      createdAt: "2026-09-18T10:05:00.000Z",
    },
    {
      id: "e1",
      event: "sign_in_failure",
      result: "failure",
      requestId: "req-1",
      remoteAddr: "192.0.2.2",
      createdAt: "2026-09-18T10:01:00.000Z",
    },
  ],
};

function problemBody(status: number) {
  return {
    type: "/problems/internal-error",
    title: "X",
    status,
    requestId: "rid",
  };
}

async function render(
  role = "super-admin",
  status: "authenticated" | "anonymous" | "error" = "authenticated",
): Promise<AdminLogsPage> {
  const page = document.createElement("admin-logs-page") as AdminLogsPage;
  (page as any).session = mockSession(status, role);
  document.body.appendChild(page);
  await page.updateComplete;
  await new Promise((r) => setTimeout(r, 10));
  await page.updateComplete;
  return page;
}

function root(page: AdminLogsPage): HTMLElement {
  return page.shadowRoot! as unknown as HTMLElement;
}

/** Settle the page after an interaction that triggers a refetch. */
async function settle(page: AdminLogsPage) {
  await page.updateComplete;
  await new Promise((r) => setTimeout(r, 10));
  await page.updateComplete;
}

function cells(page: AdminLogsPage, selector: string): string[] {
  return Array.from(root(page).querySelectorAll(selector)).map(
    (el) => el.textContent?.trim() ?? "",
  );
}

function submitForm(page: AdminLogsPage, index: number) {
  const form = root(page).querySelectorAll("form")[index]!;
  form.dispatchEvent(new Event("submit", { cancelable: true }));
}

beforeEach(() => {
  shimHistoryLocationSync();
  setHappyDOMURL("https://example.com/admin/logs");
  document.body.innerHTML = "";
});

afterEach(() => {
  for (const cleanup of navCleanups) cleanup();
  navCleanups.length = 0;
  restoreHistoryLocationSync();
  resetHappyDOMURL();
  mock.restore();
  restoreMockFetch();
});

describe("admin-logs-page", () => {
  test("anonymous visitors are redirected to /sign-in and nothing is fetched", async () => {
    let redirected = "";
    const listener = ((e: Event) => {
      redirected = (e as CustomEvent<{ path: string }>).detail.path;
    }) as EventListener;
    window.addEventListener("sf-navigate", listener);
    navCleanups.push(() => window.removeEventListener("sf-navigate", listener));

    const urls = stubStaffReads();

    await render("super-admin", "anonymous");

    expect(redirected).toBe("/sign-in");
    expect(urls.length).toBe(0);
  });

  test("below super-admin the page is forbidden and neither section is fetched", async () => {
    for (const role of ["moderator", "admin"]) {
      document.body.innerHTML = "";
      const urls = stubStaffReads();

      const page = await render(role);

      expect(root(page).querySelector(".admin-forbidden")).not.toBeNull();
      expect(root(page).querySelector(".logs-table")).toBeNull();
      expect(urls.length).toBe(0);
    }
  });

  test("a super-admin sees both sections with their default filters", async () => {
    const urls = stubStaffReads();

    const page = await render();

    expect(logsOf(urls)).toEqual(["/api/v1/staff/logs?level=warn&limit=100"]);
    expect(auditsOf(urls)).toEqual(["/api/v1/staff/audit-events?limit=50"]);

    // The log table: time, level, message, and the attribute JSON.
    expect(cells(page, ".logs-time")[0]).toBe(
      formatDateTimeNumeric("2026-09-18T10:00:00.000Z"),
    );
    // The audit table prints its timestamps through the same numeric formatter.
    expect(cells(page, ".audit-row .logs-time")[0]).toBe(
      formatDateTimeNumeric("2026-09-18T10:05:00.000Z"),
    );

    // The time columns read `timestamp`, and the audit table's technical
    // headers are English too — every one of them is rendered inside
    // lang="en".
    const heads = [...root(page).querySelectorAll(".logs-table thead th")].map(
      (th) => th.textContent?.trim(),
    );
    expect(heads.filter((h) => h === "timestamp")).toHaveLength(2);
    expect(heads).toContain(el.admin.auditActorColumn);
    expect(heads).toContain(el.admin.auditTargetColumn);
    expect(heads).toContain(el.admin.auditRequestColumn);
    const auditTable = root(page).querySelector(".audit-row")?.closest("table");
    expect(
      auditTable?.querySelectorAll("thead th span[lang='en']"),
    ).toHaveLength(5);
    expect(cells(page, ".logs-level")).toEqual(["error", "warn"]);
    expect(cells(page, ".audit-result")).toEqual(["success", "failure"]);
    expect(cells(page, ".logs-message")[0]).toBe("staff metrics totals failed");
    expect(cells(page, ".logs-fields")[0]).toContain(`"requestID":"r1"`);
    expect(cells(page, ".logs-fields")[1]).toBe(el.admin.logsFieldsEmpty);

    // The audit table: catalog event labels, wire results, and the absent
    // identities rendered from the catalog rather than as blanks.
    expect(cells(page, ".audit-event")).toEqual([
      el.admin.auditEventNames.role_changed,
      el.admin.auditEventNames.sign_in_failure,
    ]);
    // The target cell carries the id AND the role, so match on the joined
    // text rather than an exact cell value.
    expect(cells(page, ".audit-id").join("|")).toContain("actor-1");
    expect(cells(page, ".audit-id").join("|")).toContain("target-1");
    expect(cells(page, ".audit-id").join("|")).toContain(el.admin.auditNone);
    expect(cells(page, ".audit-role")).toEqual([el.account.roleModerator]);
    expect(root(page).querySelector(".logs-hint")?.textContent).toContain(
      el.admin.logsHint,
    );

    const tabs = root(page).querySelector("admin-tabs") as any;
    expect(tabs.current).toBe("logs");
  });

  test("every log level and audit result wears its own chip colour", async () => {
    stubStaffReads();

    const page = await render();

    // The colour follows the wire value through a data attribute, and the
    // word stays in the cell — colour is never the only signal.
    const levels = [
      ...root(page).querySelectorAll(".logs-level"),
    ] as HTMLElement[];
    expect(levels.map((el) => el.dataset.level)).toEqual(["error", "warn"]);
    expect(levels.map((el) => el.textContent?.trim())).toEqual([
      "error",
      "warn",
    ]);

    const results = [
      ...root(page).querySelectorAll(".audit-result"),
    ] as HTMLElement[];
    expect(results.map((el) => el.dataset.result)).toEqual([
      "success",
      "failure",
    ]);

    // The row carries the same value, so the whole line can be tinted.
    expect(
      (root(page).querySelector(".logs-row") as HTMLElement).dataset.level,
    ).toBe("error");
    expect(
      (root(page).querySelector(".audit-row") as HTMLElement).dataset.result,
    ).toBe("success");

    // happy-dom does not resolve tokens: the palette is pinned against the
    // CSS source. The colour follows the value through the chip's BORDER
    // while the word stays `--sf-text` (AA in both themes — the light
    // theme's `--sf-success` as 0.75rem text is ~3.5:1), and the chip names
    // a token in both themes rather than a per-rule hex fallback.
    const chip =
      logsStyles.match(/\.logs-level,\s*\.audit-result \{([^}]*)\}/)?.[1] ?? "";
    expect(chip).toContain("color: var(--sf-text");
    for (const [selector, token] of [
      ['\\.logs-level\\[data-level="error"\\]', "--sf-error"],
      ['\\.logs-level\\[data-level="warn"\\]', "--sf-warning"],
      ['\\.logs-level\\[data-level="info"\\]', "--sf-accent-light"],
      ['\\.logs-level\\[data-level="debug"\\]', "--sf-border"],
      ['\\.audit-result\\[data-result="success"\\]', "--sf-success"],
      ['\\.audit-result\\[data-result="failure"\\]', "--sf-error"],
    ] as const) {
      const block = logsStyles.match(new RegExp(`${selector} \\{([^}]*)\\}`));
      const body = block?.[1] ?? "";
      expect(body).toMatch(new RegExp(`border-color: var\\(${token}`));
      // The chip's WORD is only ever a text token (the AA rule): a semantic
      // status colour as 0.75rem text is ~3.5:1 in the light theme. The
      // debug chip may dim its word; the others leave it at `--sf-text`.
      const word = body.match(/(?:^|\s)color:\s*([^;]+);/)?.[1];
      if (word !== undefined) expect(word).toMatch(/var\(--sf-text/);
    }
  });

  test("the audit section carries its own scope hint and no pager", async () => {
    stubStaffReads();

    const page = await render();

    const hints = cells(page, ".logs-hint");
    expect(hints).toContain(el.admin.logsHint);
    expect(hints).toContain(el.admin.auditHint);
    // Both surfaces are tails: no load-more and no shared pager render.
    expect(root(page).querySelector("pager-nav")).toBeNull();
    expect(cells(page, ".logs-hint").length).toBe(2);
  });

  test("the URL primes both sections' filters, so a reload keeps the view", async () => {
    const urls = stubStaffReads();
    setHappyDOMURL(
      "https://example.com/admin/logs?level=error&q=boom&limit=25&aevent=sign_in_failure&alimit=10",
    );

    const page = await render();

    expect(logsOf(urls)).toEqual([
      "/api/v1/staff/logs?level=error&q=boom&limit=25",
    ]);
    expect(auditsOf(urls)).toEqual([
      "/api/v1/staff/audit-events?event=sign_in_failure&limit=10",
    ]);

    const selects = root(page).querySelectorAll(
      "select",
    ) as NodeListOf<HTMLSelectElement>;
    expect(selects[0]!.value).toBe("error");
    expect(selects[1]!.value).toBe("sign_in_failure");
  });

  test("out-of-contract URL values are ignored rather than sent", async () => {
    const urls = stubStaffReads();
    setHappyDOMURL(
      "https://example.com/admin/logs?level=trace&limit=99999&q=" +
        "a".repeat(300) +
        "&aevent=nope&alimit=9999",
    );

    await render();

    expect(logsOf(urls)).toEqual(["/api/v1/staff/logs?level=warn&limit=100"]);
    expect(auditsOf(urls)).toEqual(["/api/v1/staff/audit-events?limit=50"]);
  });

  test("applying the log filters syncs the URL and refetches only that section", async () => {
    const urls = stubStaffReads();

    const page = await render();
    expect(urls.length).toBe(2);

    // replaceState, not pushState: a filter change is not a navigation step
    // worth a back button. Compare the entry count rather than asserting an
    // absolute value — the test window is shared across files (CI parity).
    const historyBefore = window.history.length;

    const form = root(page).querySelectorAll("form")[0]!;
    (form.querySelector('select[name="level"]') as HTMLSelectElement).value =
      "error";
    (form.querySelector('input[name="q"]') as HTMLInputElement).value = "boom";
    (form.querySelector('input[name="limit"]') as HTMLInputElement).value =
      "50";
    submitForm(page, 0);
    await settle(page);

    expect(logsOf(urls)).toEqual([
      "/api/v1/staff/logs?level=warn&limit=100",
      "/api/v1/staff/logs?level=error&q=boom&limit=50",
    ]);
    // The audit section is untouched by a log filter change.
    expect(auditsOf(urls).length).toBe(1);
    expect(window.location.search).toBe(
      "?level=error&q=boom&limit=50&alimit=50",
    );
    expect(window.history.length).toBe(historyBefore);
  });

  test("applying the audit filters refetches only the audit section", async () => {
    const urls = stubStaffReads();

    const page = await render();

    const form = root(page).querySelectorAll("form")[1]!;
    (form.querySelector('select[name="event"]') as HTMLSelectElement).value =
      "role_changed";
    (form.querySelector('input[name="alimit"]') as HTMLInputElement).value =
      "10";
    submitForm(page, 1);
    await settle(page);

    expect(logsOf(urls).length).toBe(1);
    expect(auditsOf(urls)).toEqual([
      "/api/v1/staff/audit-events?limit=50",
      "/api/v1/staff/audit-events?event=role_changed&limit=10",
    ]);
    expect(window.location.search).toBe(
      "?level=warn&limit=100&aevent=role_changed&alimit=10",
    );
  });

  test("a 403 from the audit read latches the page as forbidden for good", async () => {
    // The audit read answers 403 (the server's floor moved) while the log read
    // succeeds: the log response must NOT repaint the page back to a table.
    const urls = stubStaffReads(
      async () => {
        // Delay the log read so its success resolves AFTER the audit 403.
        await new Promise((r) => setTimeout(r, 5));
        return { status: 200, body: LOGS_BODY };
      },
      async () => ({ status: 403, body: problemBody(403) }),
    );

    const page = await render();

    expect(root(page).querySelector(".admin-forbidden")).not.toBeNull();
    expect(root(page).querySelector(".logs-table")).toBeNull();
    expect(root(page).querySelector("form")).toBeNull();
    expect(urls.length).toBe(2);
  });

  test("both sections failing shows one banner per section", async () => {
    stubStaffReads(
      async () => ({ status: 500, body: problemBody(500) }),
      async () => ({ status: 500, body: problemBody(500) }),
    );

    const page = await render();

    expect(root(page).querySelectorAll("error-banner").length).toBe(2);
    expect(root(page).querySelectorAll("form").length).toBe(2);
  });

  test("an empty result in either section renders its own empty state", async () => {
    stubStaffReads(
      async () => ({ status: 200, body: { items: [] } }),
      async () => ({ status: 200, body: { items: [] } }),
    );

    const page = await render();

    expect(cells(page, ".logs-empty")).toEqual([
      el.admin.logsEmpty,
      el.admin.auditEmpty,
    ]);
    for (const empty of root(page).querySelectorAll(".logs-empty")) {
      expect(empty.getAttribute("role")).toBe("status");
    }
    expect(root(page).querySelector(".logs-table")).toBeNull();
  });

  test("both table scroll containers are keyboard reachable and heading-labelled", async () => {
    stubStaffReads();

    const page = await render();

    const regions = [...root(page).querySelectorAll(".logs-scroll")];
    expect(regions).toHaveLength(2);
    expect(regions.map((r) => r.getAttribute("aria-labelledby"))).toEqual([
      "logs-title",
      "audit-title",
    ]);
    for (const region of regions) {
      // Safari's default scrollers are not focusable — the wrapper must be.
      expect(region.getAttribute("tabindex")).toBe("0");
      expect(region.getAttribute("role")).toBe("region");
      const headingId = region.getAttribute("aria-labelledby") ?? "";
      expect(root(page).querySelector(`#${headingId}`)).not.toBeNull();
    }

    // The free-text columns' width floor is what makes the wrapper scroll on
    // a narrow window: without it table layout shrinks the columns and wraps
    // every message to one word per line. Both halves are pinned — the
    // wrapper's own overflow rule and the floor — because either alone
    // changes nothing. happy-dom resolves no layout, so the source is the
    // pin.
    const scroller = logsStyles.match(/\.logs-scroll \{([^}]*)\}/s)?.[1] ?? "";
    expect(scroller).toContain("overflow-x: auto");
    const floors =
      logsStyles.match(/\.logs-message,\s*\.logs-fields \{([^}]*)\}/s)?.[1] ??
      "";
    expect(floors).toContain("min-width: 13rem");
  });

  test("a failed log read keeps the audit section working, and retry refetches", async () => {
    let fail = true;
    const urls = stubStaffReads(async () =>
      fail
        ? { status: 500, body: problemBody(500) }
        : { status: 200, body: LOGS_BODY },
    );

    const page = await render();

    expect(root(page).querySelectorAll("error-banner").length).toBe(1);
    expect(root(page).querySelector(".audit-event")).not.toBeNull();

    fail = false;
    root(page)
      .querySelector("error-banner")!
      .dispatchEvent(new CustomEvent("retry", { bubbles: true }));
    await settle(page);

    expect(logsOf(urls).length).toBe(2);
    expect(cells(page, ".logs-message")[0]).toBe("staff metrics totals failed");
  });

  test("a failed audit read keeps the log table, and retry refetches", async () => {
    let fail = true;
    const urls = stubStaffReads(undefined, async () =>
      fail
        ? { status: 500, body: problemBody(500) }
        : { status: 200, body: AUDIT_BODY },
    );

    const page = await render();

    expect(root(page).querySelectorAll("error-banner").length).toBe(1);
    expect(cells(page, ".logs-message")[0]).toBe("staff metrics totals failed");
    expect(root(page).querySelector(".audit-event")).toBeNull();

    fail = false;
    root(page)
      .querySelector("error-banner")!
      .dispatchEvent(new CustomEvent("retry", { bubbles: true }));
    await settle(page);

    expect(auditsOf(urls).length).toBe(2);
    expect(cells(page, ".audit-event")).toContain(
      el.admin.auditEventNames.role_changed,
    );
  });
});
