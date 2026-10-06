/**
 * Admin users tab tests — the guard (anonymous
 * redirect, below-moderator forbidden), the list rendering with the
 * server-authoritative email and the URL-synced role/status filters, the
 * error state, and the member card's staged panel: one panel at a time, a
 * single save that sends only the fields that differ, and the two immediate
 * icon actions.
 */

import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";

import { el } from "@shared/catalog/el.js";
import {
  mockFetchImpl,
  mockResponse,
  mockSessionContext,
  resetHappyDOMURL,
  restoreHistoryLocationSync,
  restoreMockFetch,
  setHappyDOMURL,
  shimHistoryLocationSync,
  stripSourceComments,
  stubProperty,
} from "@shared/api/test-utils.js";

import "@features/auth/data-access/session.js";
import "./admin-users-page.js";
import type { AdminUsersPage } from "./admin-users-page.js";

const CREATED = "2023-11-14T22:13:20.000Z";

/** sf-navigate listeners registered by a test — removed in afterEach (a
 * leaked window listener outlives the shared happy-dom worker). */
const navCleanups: Array<() => void> = [];

function mockSession(
  status: "authenticated" | "anonymous" | "error" = "authenticated",
  role = "admin",
) {
  return mockSessionContext({ status, user: { role } });
}

/** Install the fetch double for one test and return every requested URL in
 * call order — a test can assert that the server was never asked. */
function stubFetch(
  handler: (
    url: string,
    method: string,
  ) => Promise<{ status: number; body: unknown }>,
): string[] {
  const urls: string[] = [];
  mockFetchImpl(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input.toString();
    urls.push(url);
    const method = init?.method ?? "GET";
    const res = await handler(url, method);
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

const LIST_BODY = {
  items: [
    {
      id: "u1",
      username: "Katakuri",
      email: "katakuri@example.com",
      role: "moderator",
      status: "active",
      emailVerified: true,
      avatarUrl: "https://example.com/media/images/9f/abc.png",
      createdAt: CREATED,
    },
    {
      id: "u2",
      username: "Beta",
      email: "beta@example.com",
      role: "user",
      status: "suspended",
      emailVerified: false,
      avatarUrl: null,
      createdAt: CREATED,
    },
  ],
  pageInfo: { hasNextPage: false, endCursor: null, total: 2 },
};

/** One row per role, so a select falling back to its first option is obvious. */
function peerBody() {
  return {
    items: [
      {
        id: "a1",
        username: "AdminTwo",
        role: "admin",
        status: "active",
        createdAt: CREATED,
      },
      {
        id: "sa1",
        username: "SuperOne",
        role: "super-admin",
        status: "active",
        createdAt: CREATED,
      },
      {
        id: "m1",
        username: "ModOne",
        role: "moderator",
        status: "active",
        createdAt: CREATED,
      },
      {
        id: "u1",
        username: "PlainUser",
        role: "user",
        status: "active",
        createdAt: CREATED,
      },
    ],
    pageInfo: { hasNextPage: false, endCursor: null, total: 4 },
  };
}

function problemBody(status: number, type = "/problems/internal-error") {
  return { type, title: "X", status, requestId: "rid" };
}

async function render(
  role = "admin",
  status: "authenticated" | "anonymous" | "error" = "authenticated",
  search = "",
): Promise<AdminUsersPage> {
  setHappyDOMURL(`https://example.com/admin/users${search}`);
  const page = document.createElement("admin-users-page") as AdminUsersPage;
  (page as any).session = mockSession(status, role);
  document.body.appendChild(page);
  await settle(page);
  return page;
}

/** Re-render + let the request microtask chain finish. */
async function settle(page: AdminUsersPage) {
  await page.updateComplete;
  await new Promise((r) => setTimeout(r, 10));
  await page.updateComplete;
}

function root(page: AdminUsersPage): HTMLElement {
  return page.shadowRoot! as unknown as HTMLElement;
}

/** The cards in list order — re-query after every state change: the render
 * replaces the nodes, so a captured element goes stale. */
function cards(page: AdminUsersPage): Element[] {
  return Array.from(root(page).querySelectorAll(".member-card"));
}

/** Open one member's panel and return the RE-QUERIED card. */
async function openMember(page: AdminUsersPage, index = 0): Promise<Element> {
  const head = cards(page)[index]?.querySelector(".member-head") as HTMLElement;
  head.click();
  await settle(page);
  return cards(page)[index]!;
}

/** The panel's role and status selects, in that order. */
function panelSelects(card: Element): HTMLSelectElement[] {
  return Array.from(
    card.querySelectorAll(".member-field select"),
  ) as HTMLSelectElement[];
}

function changeSelect(select: HTMLSelectElement, value: string) {
  select.value = value;
  select.dispatchEvent(new Event("change", { bubbles: true }));
}

function bannerText(page: AdminUsersPage): string {
  return (
    root(page).querySelector("error-banner")?.shadowRoot?.textContent ?? ""
  );
}

beforeEach(() => {
  shimHistoryLocationSync();
  window.history.replaceState(null, "", window.location.href);
  document.body.innerHTML = "";
});

afterEach(() => {
  for (const cleanup of navCleanups) cleanup();
  navCleanups.length = 0;
  restoreHistoryLocationSync();
  resetHappyDOMURL();
  mock.restore();
  restoreMockFetch();
  document.body.innerHTML = "";
});

describe("admin-users-page", () => {
  test("anonymous visitors are redirected to /sign-in and nothing is fetched", async () => {
    let redirected = "";
    const listener = ((e: Event) => {
      redirected = (e as CustomEvent<{ path: string }>).detail.path;
    }) as EventListener;
    window.addEventListener("sf-navigate", listener);
    navCleanups.push(() => window.removeEventListener("sf-navigate", listener));

    const urls = stubFetch(async () => ({ status: 200, body: LIST_BODY }));

    await render("admin", "anonymous");

    expect(redirected).toBe("/sign-in");
    expect(urls.length).toBe(0);
  });

  test("a plain user sees the forbidden state and the server is never asked", async () => {
    const urls = stubFetch(async () => ({ status: 200, body: LIST_BODY }));

    const page = await render("user");

    expect(root(page).querySelector(".admin-forbidden")).not.toBeNull();
    expect(root(page).querySelector(".members-list")).toBeNull();
    expect(urls.length).toBe(0);
  });

  test("an admin viewer renders member cards with the server-provided email", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: LIST_BODY };
    });

    const page = await render("admin");

    expect(urls[0]).toBe("/api/v1/users?limit=10");

    const list = cards(page);
    expect(list.length).toBe(2);

    expect(list[0]!.querySelector(".member-name")?.textContent?.trim()).toBe(
      "Katakuri",
    );
    expect(
      list[0]!.querySelector(".member-secondary")?.textContent?.trim(),
    ).toBe("katakuri@example.com");
    // The meta line carries the verification state + the join date; the role
    // and status moved into the chips. Both branches are pinned: the
    // first member is verified, the second is not.
    expect(list[0]!.querySelector(".member-meta")?.textContent).toContain(
      el.admin.emailVerified,
    );
    expect(list[1]!.querySelector(".member-meta")?.textContent).toContain(
      el.admin.emailUnverified,
    );

    // The avatar is the account's real image when the DTO carries one, and
    // the shared initials circle when it is null (both branches pinned: the
    // second member has no avatar).
    const avatar = list[0]!.querySelector(".member-avatar .avatar");
    expect(avatar?.tagName).toBe("IMG");
    expect(avatar?.getAttribute("src")).toBe(
      "https://example.com/media/images/9f/abc.png",
    );
    expect(list[1]!.querySelector(".member-avatar .avatar")?.textContent).toBe(
      "B",
    );

    // Role/status copy comes from the catalog (Greek), not the raw values.
    expect(
      list[0]!.querySelector(".member-chip--role")?.textContent?.trim(),
    ).toBe(el.account.roleModerator);
    expect(
      list[1]!.querySelector(".member-chip--suspended")?.textContent?.trim(),
    ).toBe(el.admin.statusSuspended);
  });

  test("email is rendered only when the server sends it (moderator projection)", async () => {
    // The server OMITS the email key for moderator viewers — the client must
    // not invent it.
    const body = {
      items: [
        {
          id: "u1",
          username: "Katakuri",
          role: "moderator",
          status: "active",
          createdAt: CREATED,
        },
      ],
      pageInfo: { hasNextPage: false, endCursor: null, total: 1 },
    };
    stubFetch(async () => ({ status: 200, body }));

    const page = await render("moderator");

    expect(cards(page)[0]!.querySelector(".member-secondary")).toBeNull();
  });

  test("each panel's selects show THAT member's values, never the first option", async () => {
    // Lit commits a select's `.value` property binding before the mapped
    // <option> children exist, so the page's updated() pass re-applies each
    // select from `data-value` once the DOM is complete; without it the
    // select would show its first option. One row per role/status, so a
    // fallback is obvious; the second member is suspended, so the status
    // select is exercised too.
    const body = {
      items: [
        {
          id: "u1",
          username: "Moderatorus",
          role: "moderator",
          status: "active",
          createdAt: CREATED,
        },
        {
          id: "u2",
          username: "Adminus",
          role: "admin",
          status: "suspended",
          createdAt: CREATED,
        },
        {
          id: "u3",
          username: "Plainus",
          role: "user",
          status: "active",
          createdAt: CREATED,
        },
      ],
      pageInfo: { hasNextPage: false, endCursor: null, total: 3 },
    };
    stubFetch(async () => ({ status: 200, body }));

    const page = await render("super-admin");

    const first = await openMember(page, 0);
    expect(panelSelects(first)[0]?.value).toBe("moderator");

    const second = await openMember(page, 1);
    expect(panelSelects(second)[0]?.value).toBe("admin");
    expect(panelSelects(second)[1]?.value).toBe("suspended");
    // One panel at a time: opening the second card closed the first.
    expect(cards(page)[0]!.querySelector(".member-panel")).toBeNull();

    const third = await openMember(page, 2);
    expect(panelSelects(third)[0]?.value).toBe("user");
  });

  test("reads role and status from the URL on mount", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: { ...LIST_BODY, items: [] } };
    });

    setHappyDOMURL(
      "https://example.com/admin/users?role=moderator&status=suspended",
    );
    const page = document.createElement("admin-users-page") as AdminUsersPage;
    (page as any).session = mockSession("authenticated", "admin");
    document.body.appendChild(page);
    await settle(page);

    expect(urls).toEqual([
      "/api/v1/users?limit=10&role=moderator&status=suspended",
    ]);

    // The filter selects carry mapped options too, so they are re-applied by
    // the same updated() pass — a filtered URL must show its own filter.
    const filters = Array.from(
      root(page).querySelectorAll(".users-filter select"),
    ) as HTMLSelectElement[];
    expect(filters[0]?.value).toBe("moderator");
    expect(filters[1]?.value).toBe("suspended");
  });

  test("invalid filter params are normalized away", async () => {
    const urls = stubFetch(async () => ({ status: 200, body: LIST_BODY }));

    const page = await render(
      "admin",
      "authenticated",
      "?role=bogus&status=bogus",
    );

    // Hand-edited garbage never reaches the API (the exact request list is
    // the load-bearing pin); the selects render the all-values option.
    expect(urls).toEqual(["/api/v1/users?limit=10"]);
    const filters = Array.from(
      root(page).querySelectorAll(".users-filter select"),
    ) as HTMLSelectElement[];
    expect(filters[0]?.value).toBe("");
    expect(filters[1]?.value).toBe("");
  });

  test("changing the role filter pushes the URL and refetches the first page", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: LIST_BODY };
    });

    const page = await render("admin");

    const roleSelect = root(page).querySelector(
      ".users-filter select",
    ) as HTMLSelectElement;
    changeSelect(roleSelect, "moderator");
    await settle(page);

    expect(window.location.search).toContain("role=moderator");
    expect(urls[urls.length - 1]).toBe("/api/v1/users?limit=10&role=moderator");
  });

  test("a filter change drops the open panel and its staged edits", async () => {
    stubFetch(async () => ({ status: 200, body: LIST_BODY }));

    const page = await render("admin");
    const card = await openMember(page, 0);
    changeSelect(panelSelects(card)[0]!, "user");
    await settle(page);
    expect(root(page).querySelector(".member-chip--staged")).not.toBeNull();

    const roleSelect = root(page).querySelector(
      ".users-filter select",
    ) as HTMLSelectElement;
    changeSelect(roleSelect, "moderator");
    await settle(page);

    // These are new rows: neither the panel nor a draft keyed by the previous
    // page's member may survive onto them.
    expect(root(page).querySelector(".member-panel")).toBeNull();
    expect(root(page).querySelector(".member-chip--staged")).toBeNull();
  });

  test("a failed load shows the error banner with retry", async () => {
    stubFetch(async () => ({ status: 500, body: problemBody(500) }));

    const page = await render("admin");

    expect(
      root(page)
        .querySelector("error-banner")
        ?.shadowRoot?.querySelector(".error"),
    ).not.toBeNull();
  });

  test("an empty result set renders the catalog-backed empty state", async () => {
    stubFetch(async () => ({
      status: 200,
      body: {
        items: [],
        pageInfo: { hasNextPage: false, endCursor: null, total: 0 },
      },
    }));

    const page = await render("admin");

    const empty = root(page).querySelector(".admin-empty");
    expect(empty?.textContent?.trim()).toBe(el.admin.usersEmpty);
    expect(empty?.getAttribute("role")).toBe("status");
    expect(root(page).querySelector(".members-list")).toBeNull();
    expect(root(page).querySelector("pager-nav")).toBeNull();
    // The reserved-footprint wrapper.
    expect(empty?.closest(".state-block")).not.toBeNull();
  });

  test("exactly ONE list fetch per authenticated mount", async () => {
    // Regression pin, mirroring the admin-page test: syncOnConnect:false +
    // the authenticated latch must issue ONE users-list request per mount.
    let calls = 0;
    stubFetch(async () => {
      calls++;
      return { status: 200, body: LIST_BODY };
    });

    const page = await render("admin");

    expect(root(page).querySelector(".members-list")).not.toBeNull();
    expect(calls).toBe(1);
  });

  // ── The staged panel ─────────────────────────────────────────────

  test("only the rows this viewer may act on open a panel", async () => {
    stubFetch(async () => ({ status: 200, body: peerBody() }));

    // An admin acts on moderator/user rows only: the admin and
    // super-admin rows render a static head — no chevron, nothing to open.
    const adminPage = await render("admin");
    const adminCards = cards(adminPage);
    expect(adminCards.length).toBe(4);
    expect(adminCards[0]!.querySelector(".member-head--static")).not.toBeNull();
    expect(adminCards[1]!.querySelector(".member-head--static")).not.toBeNull();
    expect(adminCards[2]!.querySelector(".member-head--static")).toBeNull();
    expect(adminCards[3]!.querySelector(".member-head--static")).toBeNull();
    for (const card of adminCards) {
      expect(card.querySelector(".member-chevron") !== null).toBe(
        card.querySelector(".member-head--static") === null,
      );
    }

    // The manageable rows open a panel holding both staged selects and the
    // two immediate icon actions.
    const managed = await openMember(adminPage, 2);
    expect(panelSelects(managed).length).toBe(2);
    const actions = managed.querySelectorAll(".member-action");
    expect(actions.length).toBe(2);
    expect(actions[0]!.getAttribute("aria-label")).toBe(el.admin.resetButton);
    expect(actions[1]!.getAttribute("aria-label")).toBe(el.admin.deleteButton);

    // A super-admin acts on every row ("anyone incl. self").
    const saPage = await render("super-admin");
    for (const card of cards(saPage)) {
      expect(card.querySelector(".member-head--static")).toBeNull();
    }

    // A moderator sees no admin+ control: only the USER row opens a panel,
    // and that panel carries the status select ALONE with no icon actions.
    const modPage = await render("moderator");
    const modCards = cards(modPage);
    expect(modCards[0]!.querySelector(".member-head--static")).not.toBeNull(); // admin
    expect(modCards[1]!.querySelector(".member-head--static")).not.toBeNull(); // super-admin
    expect(modCards[2]!.querySelector(".member-head--static")).not.toBeNull(); // moderator
    expect(modCards[3]!.querySelector(".member-head--static")).toBeNull(); // user
    const modPanel = await openMember(modPage, 3);
    expect(panelSelects(modPanel).length).toBe(1);
    expect(panelSelects(modPanel)[0]?.value).toBe("active");
    expect(modPanel.querySelector(".member-action")).toBeNull();
  });

  test("the role select offers only the roles the viewer may set", async () => {
    stubFetch(async () => ({ status: 200, body: peerBody() }));

    const adminPage = await render("admin");
    const adminPanel = await openMember(adminPage, 2);
    expect(
      Array.from(panelSelects(adminPanel)[0]!.options).map((o) => o.value),
    ).toEqual(["moderator", "user"]);

    const saPage = await render("super-admin");
    const saPanel = await openMember(saPage, 0);
    expect(
      Array.from(panelSelects(saPanel)[0]!.options).map((o) => o.value),
    ).toEqual(["user", "moderator", "admin", "super-admin"]);
  });

  test("a staged role change sends nothing until the ONE save", async () => {
    const requests: Array<{ url: string; method: string }> = [];
    stubFetch(async (url, method) => {
      requests.push({ url, method });
      if (method === "PATCH") {
        return { status: 200, body: { ...LIST_BODY.items[0], role: "user" } };
      }
      return { status: 200, body: LIST_BODY };
    });

    const page = await render("admin");
    const card = await openMember(page, 0);
    expect(card.querySelector(".member-dirty")?.textContent?.trim()).toBe(
      el.admin.memberNoChange,
    );
    expect(card.querySelector(".member-save")).toBeNull();

    changeSelect(panelSelects(card)[0]!, "user");
    await settle(page);

    // Staged, not sent: the save appears only now, and the chip reads the
    // value the save WOULD send (dashed border).
    expect(requests.filter((r) => r.method === "PATCH").length).toBe(0);
    const staged = cards(page)[0]!;
    expect(staged.querySelector(".member-save")).not.toBeNull();
    const chip = staged.querySelector(".member-chip--role")!;
    expect(chip.textContent?.trim()).toBe(el.account.roleUser);
    expect(chip.classList.contains("member-chip--staged")).toBe(true);
    expect(staged.querySelector(".member-dirty")?.textContent).toContain(
      `${el.admin.roleSelectLabel}: ${el.account.roleUser}`,
    );

    (staged.querySelector(".member-save") as HTMLButtonElement).click();
    await settle(page);

    const patch = requests.find((r) => r.method === "PATCH");
    expect(patch?.url).toBe("/api/v1/users/u1/role");

    // The card now carries the server's projection and nothing is pending.
    const after = cards(page)[0]!;
    expect(after.querySelector(".member-chip--role")?.textContent?.trim()).toBe(
      el.account.roleUser,
    );
    expect(after.querySelector(".member-chip--staged")).toBeNull();
    expect(after.querySelector(".member-save")).toBeNull();
    expect(after.querySelector(".member-dirty")?.textContent?.trim()).toBe(
      el.admin.memberNoChange,
    );
  });

  test("a save sends role and status as the two operations they are", async () => {
    const restoreConfirm = stubProperty(
      window,
      "confirm",
      mock(() => true),
    );
    try {
      const requests: Array<{ url: string; method: string }> = [];
      stubFetch(async (url, method) => {
        requests.push({ url, method });
        if (method === "PATCH") {
          return {
            status: 200,
            body: { ...LIST_BODY.items[0], role: "user" },
          };
        }
        if (method === "POST") {
          return {
            status: 200,
            body: { ...LIST_BODY.items[0], role: "user", status: "suspended" },
          };
        }
        return { status: 200, body: LIST_BODY };
      });

      const page = await render("admin");
      const card = await openMember(page, 0);
      const selects = panelSelects(card);
      changeSelect(selects[0]!, "user");
      await settle(page);
      changeSelect(panelSelects(cards(page)[0]!)[1]!, "suspended");
      await settle(page);

      // One press for both changes; the suspended transition is the one that
      // asks (it logs the target out).
      expect(window.confirm).toHaveBeenCalledTimes(0);
      (
        cards(page)[0]!.querySelector(".member-save") as HTMLButtonElement
      ).click();
      await settle(page);
      expect(window.confirm).toHaveBeenCalledTimes(1);

      const writes = requests.filter((r) => r.method !== "GET");
      expect(writes.map((r) => `${r.method} ${r.url}`)).toEqual([
        "PATCH /api/v1/users/u1/role",
        "POST /api/v1/users/u1/suspend",
      ]);

      const after = cards(page)[0]!;
      expect(
        after.querySelector(".member-chip--role")?.textContent?.trim(),
      ).toBe(el.account.roleUser);
      expect(
        after.querySelector(".member-chip--suspended")?.textContent?.trim(),
      ).toBe(el.admin.statusSuspended);
      expect(after.querySelector(".member-chip--staged")).toBeNull();
      expect(after.querySelector(".member-save")).toBeNull();
    } finally {
      restoreConfirm();
    }
  });

  test("a declined suspend confirmation sends nothing and keeps the draft", async () => {
    const restoreConfirm = stubProperty(
      window,
      "confirm",
      mock(() => false),
    );
    try {
      const writes: string[] = [];
      stubFetch(async (url, method) => {
        if (method !== "GET") writes.push(`${method} ${url}`);
        return { status: 200, body: LIST_BODY };
      });

      const page = await render("admin");
      const card = await openMember(page, 0);
      changeSelect(panelSelects(card)[1]!, "suspended");
      await settle(page);
      (
        cards(page)[0]!.querySelector(".member-save") as HTMLButtonElement
      ).click();
      await settle(page);

      expect(writes).toEqual([]);
      // The staged status survives the refusal, so a second press can save it.
      const after = cards(page)[0]!;
      expect(panelSelects(after)[1]?.value).toBe("suspended");
      expect(after.querySelector(".member-save")).not.toBeNull();
    } finally {
      restoreConfirm();
    }
  });

  test("reactivating needs no confirmation and replaces the card", async () => {
    const restoreConfirm = stubProperty(
      window,
      "confirm",
      mock(() => true),
    );
    try {
      const requests: Array<{ url: string; method: string }> = [];
      stubFetch(async (url, method) => {
        requests.push({ url, method });
        if (method === "POST") {
          return {
            status: 200,
            body: { ...LIST_BODY.items[1], status: "active" },
          };
        }
        return { status: 200, body: LIST_BODY };
      });

      const page = await render("admin");
      const card = await openMember(page, 1);
      changeSelect(panelSelects(card)[1]!, "active");
      await settle(page);
      (
        cards(page)[1]!.querySelector(".member-save") as HTMLButtonElement
      ).click();
      await settle(page);

      expect(window.confirm).toHaveBeenCalledTimes(0);
      expect(
        requests.some(
          (r) => r.method === "POST" && r.url === "/api/v1/users/u2/reactivate",
        ),
      ).toBe(true);
      expect(
        cards(page)[1]!
          .querySelector(".member-chip--active")
          ?.textContent?.trim(),
      ).toBe(el.admin.statusActive);
    } finally {
      restoreConfirm();
    }
  });

  test("a save that lands its role and then fails the status keeps the applied half", async () => {
    const restoreConfirm = stubProperty(
      window,
      "confirm",
      mock(() => true),
    );
    try {
      stubFetch(async (_url, method) => {
        if (method === "PATCH") {
          return { status: 200, body: { ...LIST_BODY.items[0], role: "user" } };
        }
        if (method === "POST") {
          return {
            status: 409,
            body: problemBody(409, "/problems/auth/last-super-admin"),
          };
        }
        return { status: 200, body: LIST_BODY };
      });

      const page = await render("admin");
      const card = await openMember(page, 0);
      changeSelect(panelSelects(card)[0]!, "user");
      await settle(page);
      changeSelect(panelSelects(cards(page)[0]!)[1]!, "suspended");
      await settle(page);
      (
        cards(page)[0]!.querySelector(".member-save") as HTMLButtonElement
      ).click();
      await settle(page);

      expect(bannerText(page)).toContain(
        el.problems["/problems/auth/last-super-admin"],
      );
      // The role the server confirmed is real, so the card shows it and the
      // draft drops it; only the failed status stays staged for a retry.
      const after = cards(page)[0]!;
      expect(
        after.querySelector(".member-chip--role")?.textContent?.trim(),
      ).toBe(el.account.roleUser);
      const statusChip = after.querySelector(".member-chip--suspended")!;
      expect(statusChip.classList.contains("member-chip--staged")).toBe(true);
      expect(after.querySelector(".member-save")).not.toBeNull();
    } finally {
      restoreConfirm();
    }
  });

  test("a guarded role change (409) shows the last-super-admin message", async () => {
    stubFetch(async (_url, method) => {
      if (method === "PATCH") {
        return {
          status: 409,
          body: problemBody(409, "/problems/auth/last-super-admin"),
        };
      }
      return { status: 200, body: LIST_BODY };
    });

    const page = await render("admin");
    const card = await openMember(page, 0);
    changeSelect(panelSelects(card)[0]!, "user");
    await settle(page);
    (
      cards(page)[0]!.querySelector(".member-save") as HTMLButtonElement
    ).click();
    await settle(page);

    expect(bannerText(page)).toContain(
      el.problems["/problems/auth/last-super-admin"],
    );
  });

  test("a guarded suspend (409) shows the last-super-admin message", async () => {
    // A STATUS-ONLY save (the role is untouched, so nothing is staged for
    // it) must surface the guard's problem through the same banner — the
    // status leg of a two-leg save is pinned separately.
    const restoreConfirm = stubProperty(
      window,
      "confirm",
      mock(() => true),
    );
    try {
      stubFetch(async (_url, method) => {
        if (method === "POST") {
          return {
            status: 409,
            body: problemBody(409, "/problems/auth/last-super-admin"),
          };
        }
        return { status: 200, body: LIST_BODY };
      });

      const page = await render("super-admin");
      const card = await openMember(page, 0);
      changeSelect(panelSelects(card)[1]!, "suspended");
      await settle(page);
      (
        cards(page)[0]!.querySelector(".member-save") as HTMLButtonElement
      ).click();
      await settle(page);

      expect(bannerText(page)).toContain(
        el.problems["/problems/auth/last-super-admin"],
      );
      // The refused status stays staged, so a retry can send it again.
      const after = cards(page)[0]!;
      expect(panelSelects(after)[1]?.value).toBe("suspended");
      expect(after.querySelector(".member-save")).not.toBeNull();
    } finally {
      restoreConfirm();
    }
  });

  test("the panel's controls disable while its save is in flight", async () => {
    let resolvePatch: (r: { status: number; body: unknown }) => void = () => {};
    stubFetch(async (_url, method) => {
      if (method === "PATCH") {
        // Hold the PATCH open so the in-flight state is observable.
        return new Promise((res) => {
          resolvePatch = res;
        });
      }
      return { status: 200, body: LIST_BODY };
    });

    const page = await render("admin");
    const card = await openMember(page, 0);
    changeSelect(panelSelects(card)[0]!, "user");
    await settle(page);
    (
      cards(page)[0]!.querySelector(".member-save") as HTMLButtonElement
    ).click();
    await settle(page);

    const inFlight = cards(page)[0]!;
    expect(
      (inFlight.querySelector(".member-save") as HTMLButtonElement).disabled,
    ).toBe(true);
    for (const select of panelSelects(inFlight)) {
      expect(select.disabled).toBe(true);
    }

    resolvePatch({
      status: 200,
      body: { ...LIST_BODY.items[0], role: "user" },
    });
    await settle(page);

    const settled = cards(page)[0]!;
    for (const select of panelSelects(settled)) {
      expect(select.disabled).toBe(false);
    }
  });

  test("a save never confirms while another save is in flight", async () => {
    const confirmMock = mock(() => true);
    const restoreConfirm = stubProperty(window, "confirm", confirmMock);
    try {
      let resolvePatch: (r: {
        status: number;
        body: unknown;
      }) => void = () => {};
      const posts: string[] = [];
      const body = {
        items: [
          {
            id: "u1",
            username: "One",
            role: "user",
            status: "active",
            createdAt: CREATED,
          },
          {
            id: "u2",
            username: "Two",
            role: "user",
            status: "active",
            createdAt: CREATED,
          },
        ],
        pageInfo: { hasNextPage: false, endCursor: null, total: 2 },
      };
      stubFetch(async (url, method) => {
        if (method === "PATCH") {
          return new Promise((res) => {
            resolvePatch = res;
          });
        }
        if (method === "POST") {
          posts.push(url);
          return {
            status: 200,
            body: { ...body.items[1], status: "suspended" },
          };
        }
        return { status: 200, body };
      });

      const page = await render("admin");
      const first = await openMember(page, 0);
      changeSelect(panelSelects(first)[0]!, "moderator");
      await settle(page);
      (
        cards(page)[0]!.querySelector(".member-save") as HTMLButtonElement
      ).click();
      await settle(page);

      // The first save holds the page's save slot; the second member's
      // suspend save is dropped before its confirm gate runs.
      await openMember(page, 1);
      changeSelect(panelSelects(cards(page)[1]!)[1]!, "suspended");
      await settle(page);
      (
        cards(page)[1]!.querySelector(".member-save") as HTMLButtonElement
      ).click();
      await settle(page);

      expect(window.confirm).toHaveBeenCalledTimes(0);
      expect(posts.length).toBe(0);

      resolvePatch({
        status: 200,
        body: { ...body.items[0], role: "moderator" },
      });
      await settle(page);

      // The slot is free: the staged suspend now reaches its confirm gate.
      (
        cards(page)[1]!.querySelector(".member-save") as HTMLButtonElement
      ).click();
      await settle(page);
      expect(window.confirm).toHaveBeenCalledTimes(1);
      expect(posts).toEqual(["/api/v1/users/u2/suspend"]);
    } finally {
      restoreConfirm();
    }
  });

  test("«Άκυρο» drops the staged edit, and closing the panel drops it too", async () => {
    stubFetch(async () => ({ status: 200, body: LIST_BODY }));

    const page = await render("admin");
    const card = await openMember(page, 0);
    changeSelect(panelSelects(card)[0]!, "user");
    await settle(page);
    (
      cards(page)[0]!.querySelector(".member-cancel") as HTMLButtonElement
    ).click();
    await settle(page);

    let after = cards(page)[0]!;
    expect(after.querySelector(".member-chip--role")?.textContent?.trim()).toBe(
      el.account.roleModerator,
    );
    expect(after.querySelector(".member-save")).toBeNull();
    // The panel stays open — cancelling is not closing.
    expect(after.querySelector(".member-panel")).not.toBeNull();

    changeSelect(panelSelects(after)[0]!, "user");
    await settle(page);
    (cards(page)[0]!.querySelector(".member-head") as HTMLElement).click();
    await settle(page);
    expect(cards(page)[0]!.querySelector(".member-panel")).toBeNull();

    const reopened = await openMember(page, 0);
    expect(reopened.querySelector(".member-save")).toBeNull();
    expect(
      reopened.querySelector(".member-chip--role")?.textContent?.trim(),
    ).toBe(el.account.roleModerator);
  });

  test("opening another member's panel drops the outgoing draft", async () => {
    stubFetch(async () => ({ status: 200, body: LIST_BODY }));

    const page = await render("admin");
    const first = await openMember(page, 0);
    changeSelect(panelSelects(first)[0]!, "user");
    await settle(page);
    expect(
      cards(page)[0]!.querySelector(".member-chip--staged"),
    ).not.toBeNull();

    await openMember(page, 1);
    expect(cards(page)[0]!.querySelector(".member-chip--staged")).toBeNull();

    const reopened = await openMember(page, 0);
    expect(reopened.querySelector(".member-save")).toBeNull();
  });

  test("the panel shows the leave hint only while a draft is staged", async () => {
    stubFetch(async (_url, method) => {
      if (method === "PATCH") {
        return { status: 200, body: { ...LIST_BODY.items[0], role: "user" } };
      }
      return { status: 200, body: LIST_BODY };
    });

    const page = await render("admin");
    const card = await openMember(page, 0);
    expect(card.querySelector(".member-leave-hint")).toBeNull();

    changeSelect(panelSelects(card)[0]!, "user");
    await settle(page);
    expect(
      cards(page)[0]!.querySelector(".member-leave-hint")?.textContent?.trim(),
    ).toBe(el.admin.memberLeaveHint);

    (
      cards(page)[0]!.querySelector(".member-cancel") as HTMLButtonElement
    ).click();
    await settle(page);
    expect(cards(page)[0]!.querySelector(".member-leave-hint")).toBeNull();

    changeSelect(panelSelects(cards(page)[0]!)[0]!, "user");
    await settle(page);
    (
      cards(page)[0]!.querySelector(".member-save") as HTMLButtonElement
    ).click();
    await settle(page);
    expect(cards(page)[0]!.querySelector(".member-leave-hint")).toBeNull();
  });

  // ── Password reset ───────────────────────────────────────────────

  test("a declined reset confirmation sends nothing", async () => {
    const restoreConfirm = stubProperty(
      window,
      "confirm",
      mock(() => false),
    );
    try {
      const posts: string[] = [];
      stubFetch(async (url, method) => {
        if (method === "POST") posts.push(url);
        return { status: 200, body: LIST_BODY };
      });

      const page = await render("admin");
      const card = await openMember(page, 0);
      (card.querySelector(".member-action") as HTMLButtonElement).click();
      await settle(page);

      expect(posts.length).toBe(0);
      expect(root(page).querySelector(".reset-dialog")).toBeNull();
    } finally {
      restoreConfirm();
    }
  });

  test("a confirmed reset posts, shows the one-time password, and closes", async () => {
    const restoreConfirm = stubProperty(
      window,
      "confirm",
      mock(() => true),
    );
    try {
      const requests: Array<{ url: string; method: string }> = [];
      stubFetch(async (url, method) => {
        requests.push({ url, method });
        if (url.endsWith("/reset-password")) {
          return { status: 200, body: { password: "AbcDef2345HjkLmn" } };
        }
        return { status: 200, body: LIST_BODY };
      });

      const page = await render("admin");
      const card = await openMember(page, 0);
      (card.querySelector(".member-action") as HTMLButtonElement).click();
      await settle(page);

      const post = requests.find((r) => r.method === "POST");
      expect(post?.url).toBe("/api/v1/users/u1/reset-password");

      // The one-time password dialog shows the plaintext + catalog copy.
      const dialog = root(page).querySelector(".reset-dialog");
      expect(dialog).not.toBeNull();
      expect(dialog?.querySelector("code")?.textContent).toBe(
        "AbcDef2345HjkLmn",
      );
      expect(
        dialog?.querySelector(".reset-dialog-title")?.textContent,
      ).toContain(el.admin.resetResultTitle);

      // Close clears the plaintext from the DOM.
      const close = dialog?.querySelector(".reset-close") as HTMLButtonElement;
      close.click();
      await settle(page);
      expect(root(page).querySelector(".reset-dialog")).toBeNull();
    } finally {
      restoreConfirm();
    }
  });

  test("the reset plaintext does not survive a load", async () => {
    const restoreConfirm = stubProperty(
      window,
      "confirm",
      mock(() => true),
    );
    try {
      stubFetch(async (url) => {
        if (url.endsWith("/reset-password")) {
          return { status: 200, body: { password: "AbcDef2345HjkLmn" } };
        }
        return { status: 200, body: LIST_BODY };
      });

      const page = await render("admin");
      const card = await openMember(page, 0);
      (card.querySelector(".member-action") as HTMLButtonElement).click();
      await settle(page);
      expect(root(page).querySelector(".reset-dialog")).not.toBeNull();

      // A new load closes the dialog with its plaintext: the password must
      // not outlive the list context that produced it.
      const roleSelect = root(page).querySelector(
        ".users-filter select",
      ) as HTMLSelectElement;
      changeSelect(roleSelect, "moderator");
      await settle(page);

      expect(root(page).querySelector(".reset-dialog")).toBeNull();
    } finally {
      restoreConfirm();
    }
  });

  test("the reset result is a native modal dialog that Escape dismisses", async () => {
    const restoreConfirm = stubProperty(
      window,
      "confirm",
      mock(() => true),
    );
    try {
      stubFetch(async (url) => {
        if (url.endsWith("/reset-password")) {
          return { status: 200, body: { password: "AbcDef2345HjkLmn" } };
        }
        return { status: 200, body: LIST_BODY };
      });

      const page = await render("admin");
      const card = await openMember(page, 0);
      (card.querySelector(".member-action") as HTMLButtonElement).click();
      await settle(page);

      const dialog = root(page).querySelector(
        ".reset-dialog",
      ) as HTMLDialogElement;
      expect(dialog.tagName).toBe("DIALOG");
      expect(dialog.getAttribute("aria-labelledby")).toBe("reset-dialog-title");
      // The native element owns modality — no hand-rolled role/aria-modal.
      expect(dialog.getAttribute("role")).toBeNull();
      expect(dialog.getAttribute("aria-modal")).toBeNull();
      // happy-dom's showModal() is a stub that sets the open attribute, so
      // the markup is what the test DOM can observe; the guard itself is
      // pinned against the source below (docs/patterns/web/testing.md).
      expect(dialog.hasAttribute("open")).toBe(true);

      // Escape arrives as the dialog's cancel event: the state clears and
      // the plaintext leaves the DOM with the element.
      dialog.dispatchEvent(new Event("cancel"));
      await settle(page);
      expect(root(page).querySelector(".reset-dialog")).toBeNull();

      const source = stripSourceComments(
        await Bun.file(
          new URL("./admin-users-page.ts", import.meta.url),
        ).text(),
      );
      expect(source).toMatch(/typeof\s+\w+\.showModal\s*===\s*"function"/);
    } finally {
      restoreConfirm();
    }
  });

  test("the reset dialog falls back to the open attribute without showModal", async () => {
    const restoreConfirm = stubProperty(
      window,
      "confirm",
      mock(() => true),
    );
    const proto = HTMLDialogElement.prototype as {
      showModal?: () => void;
    };
    const descriptor = Object.getOwnPropertyDescriptor(proto, "showModal");
    delete proto.showModal;
    try {
      stubFetch(async (url) => {
        if (url.endsWith("/reset-password")) {
          return { status: 200, body: { password: "AbcDef2345HjkLmn" } };
        }
        return { status: 200, body: LIST_BODY };
      });

      const page = await render("admin");
      const card = await openMember(page, 0);
      (card.querySelector(".member-action") as HTMLButtonElement).click();
      await settle(page);

      // The DOM without the method: the fallback marks the dialog open —
      // the portability branch components.md rule 10 records.
      const dialog = root(page).querySelector(
        ".reset-dialog",
      ) as HTMLDialogElement;
      expect(dialog.hasAttribute("open")).toBe(true);
    } finally {
      // Restore the exact descriptor (the prototype's is non-enumerable).
      if (descriptor) {
        Object.defineProperty(proto, "showModal", descriptor);
      }
      restoreConfirm();
    }
  });

  test("a pending reset names itself on the icon button", async () => {
    const restoreConfirm = stubProperty(
      window,
      "confirm",
      mock(() => true),
    );
    try {
      let resolveReset: (r: {
        status: number;
        body: unknown;
      }) => void = () => {};
      stubFetch(async (_url, method) => {
        if (method === "POST") {
          return new Promise((res) => {
            resolveReset = res;
          });
        }
        return { status: 200, body: LIST_BODY };
      });

      const page = await render("admin");
      const card = await openMember(page, 0);
      (card.querySelector(".member-action") as HTMLButtonElement).click();
      await settle(page);

      // The icon cannot change, so the pending state reaches the user through
      // the label the icon carries (and through the disabled state).
      const pending = cards(page)[0]!.querySelector(
        ".member-action",
      ) as HTMLButtonElement;
      expect(pending.disabled).toBe(true);
      expect(pending.getAttribute("aria-label")).toBe(el.admin.resetPending);
      expect(pending.getAttribute("title")).toBe(el.admin.resetPending);

      resolveReset({ status: 200, body: { password: "AbcDef2345HjkLmn" } });
      await settle(page);
      expect(
        cards(page)[0]!
          .querySelector(".member-action")
          ?.getAttribute("aria-label"),
      ).toBe(el.admin.resetButton);
    } finally {
      restoreConfirm();
    }
  });

  test("a stale in-flight reset never opens its dialog", async () => {
    const restoreConfirm = stubProperty(
      window,
      "confirm",
      mock(() => true),
    );
    try {
      let resolveReset: (r: {
        status: number;
        body: unknown;
      }) => void = () => {};
      stubFetch(async (url) => {
        if (url.endsWith("/reset-password")) {
          return new Promise((res) => {
            resolveReset = res;
          });
        }
        return { status: 200, body: LIST_BODY };
      });

      const page = await render("admin");
      const card = await openMember(page, 0);
      (card.querySelector(".member-action") as HTMLButtonElement).click();
      await settle(page);

      // The reset is still in flight when a filter change loads a new list;
      // its late response must not paint the dialog over that list.
      const roleSelect = root(page).querySelector(
        ".users-filter select",
      ) as HTMLSelectElement;
      changeSelect(roleSelect, "moderator");
      await settle(page);

      resolveReset({ status: 200, body: { password: "AbcDef2345HjkLmn" } });
      await settle(page);

      expect(root(page).querySelector(".reset-dialog")).toBeNull();
    } finally {
      restoreConfirm();
    }
  });

  test("a failed reset shows the error banner and keeps the list", async () => {
    const restoreConfirm = stubProperty(
      window,
      "confirm",
      mock(() => true),
    );
    try {
      stubFetch(async (_url, method) => {
        if (method === "POST") return { status: 500, body: problemBody(500) };
        return { status: 200, body: LIST_BODY };
      });

      const page = await render("admin");
      const card = await openMember(page, 0);
      (card.querySelector(".member-action") as HTMLButtonElement).click();
      await settle(page);

      expect(bannerText(page).length).toBeGreaterThan(0);
      expect(root(page).querySelector(".members-list")).not.toBeNull();
      expect(root(page).querySelector(".reset-dialog")).toBeNull();
    } finally {
      restoreConfirm();
    }
  });

  // ── Deletion ─────────────────────────────────────────────────────

  test("a confirmed delete removes the card, its panel and its draft", async () => {
    const restoreConfirm = stubProperty(
      window,
      "confirm",
      mock(() => true),
    );
    try {
      const requests: Array<{ url: string; method: string }> = [];
      stubFetch(async (url, method) => {
        requests.push({ url, method });
        if (method === "DELETE") return { status: 204, body: null };
        return { status: 200, body: LIST_BODY };
      });

      const page = await render("admin");
      const card = await openMember(page, 0);
      changeSelect(panelSelects(card)[0]!, "user");
      await settle(page);
      (
        cards(page)[0]!.querySelector(
          ".member-action--danger",
        ) as HTMLButtonElement
      ).click();
      await settle(page);

      expect(
        requests.some(
          (r) => r.method === "DELETE" && r.url === "/api/v1/users/u1",
        ),
      ).toBe(true);
      // Two cards before, one after: only Beta (u2) remains.
      const rest = cards(page);
      expect(rest.length).toBe(1);
      expect(rest[0]!.querySelector(".member-name")?.textContent?.trim()).toBe(
        "Beta",
      );
      // The deleted member's panel and staged edit went with the row.
      expect(root(page).querySelector(".member-panel")).toBeNull();
      expect(root(page).querySelector(".member-chip--staged")).toBeNull();
    } finally {
      restoreConfirm();
    }
  });

  test("deleting the last row re-syncs to the empty state", async () => {
    const restoreConfirm = stubProperty(
      window,
      "confirm",
      mock(() => true),
    );
    try {
      let deleted = false;
      let listCalls = 0;
      const requests: Array<{ url: string; method: string }> = [];
      stubFetch(async (url, method) => {
        requests.push({ url, method });
        if (method === "DELETE") {
          deleted = true;
          return { status: 204, body: null };
        }
        listCalls++;
        return {
          status: 200,
          body: deleted
            ? {
                items: [],
                pageInfo: { hasNextPage: false, endCursor: null, total: 0 },
              }
            : {
                items: [LIST_BODY.items[0]],
                pageInfo: { hasNextPage: false, endCursor: null, total: 1 },
              },
        };
      });

      const page = await render("admin");
      expect(cards(page).length).toBe(1);
      const card = await openMember(page, 0);
      (
        card.querySelector(".member-action--danger") as HTMLButtonElement
      ).click();
      await settle(page);

      expect(
        requests.some(
          (r) => r.method === "DELETE" && r.url === "/api/v1/users/u1",
        ),
      ).toBe(true);
      // No previous page to step back to: the page re-syncs, and the second
      // list load paints the empty state.
      expect(listCalls).toBe(2);
      expect(root(page).querySelector(".admin-empty")).not.toBeNull();
      expect(root(page).querySelector(".members-list")).toBeNull();
    } finally {
      restoreConfirm();
    }
  });

  test("deleting the last row on a previous-page entry steps back", async () => {
    const restoreConfirm = stubProperty(
      window,
      "confirm",
      mock(() => true),
    );
    const back = mock(() => {});
    const restoreBack = stubProperty(window.history, "back", back);
    try {
      let listCalls = 0;
      stubFetch(async (_url, method) => {
        if (method === "DELETE") return { status: 204, body: null };
        listCalls++;
        return {
          status: 200,
          body: {
            items: [LIST_BODY.items[0]],
            pageInfo: { hasNextPage: false, endCursor: null, total: 1 },
          },
        };
      });

      // A page-2 history entry created by our own Next: Previous is live.
      setHappyDOMURL("https://example.com/admin/users");
      window.history.pushState({ sfPager: true }, "", "/admin/users");
      const page = await render("admin");
      expect(cards(page).length).toBe(1);
      const card = await openMember(page, 0);
      (
        card.querySelector(".member-action--danger") as HTMLButtonElement
      ).click();
      await settle(page);

      // The prior cursor lives in the history entry: stepping back is
      // history.back(), not another load of the current cursor.
      expect(back).toHaveBeenCalledTimes(1);
      expect(listCalls).toBe(1);
    } finally {
      restoreBack();
      restoreConfirm();
    }
  });

  test("a declined delete confirmation sends nothing", async () => {
    const restoreConfirm = stubProperty(
      window,
      "confirm",
      mock(() => false),
    );
    try {
      const deletes: string[] = [];
      stubFetch(async (url, method) => {
        if (method === "DELETE") deletes.push(url);
        return { status: 200, body: LIST_BODY };
      });

      const page = await render("admin");
      const card = await openMember(page, 0);
      (
        card.querySelector(".member-action--danger") as HTMLButtonElement
      ).click();
      await settle(page);

      expect(deletes.length).toBe(0);
      expect(cards(page).length).toBe(2);
    } finally {
      restoreConfirm();
    }
  });

  test("a guarded delete (409) shows the last-super-admin message", async () => {
    const restoreConfirm = stubProperty(
      window,
      "confirm",
      mock(() => true),
    );
    try {
      stubFetch(async (_url, method) => {
        if (method === "DELETE") {
          return {
            status: 409,
            body: problemBody(409, "/problems/auth/last-super-admin"),
          };
        }
        return { status: 200, body: LIST_BODY };
      });

      const page = await render("admin");
      const card = await openMember(page, 0);
      (
        card.querySelector(".member-action--danger") as HTMLButtonElement
      ).click();
      await settle(page);

      expect(bannerText(page)).toContain(
        el.problems["/problems/auth/last-super-admin"],
      );
      expect(cards(page).length).toBe(2);
    } finally {
      restoreConfirm();
    }
  });
});
