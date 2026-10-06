/**
 * Admin tab chrome tests — the tab row is the same
 * for every staff surface, except the super-admin-only logs tab. Its
 * visibility is role-driven (the element reads the session context rather
 * than trusting each page to pass a flag), so these cases live here.
 */

import { afterEach, describe, expect, test } from "bun:test";

import { el } from "@shared/catalog/el.js";
import { stripCssComments } from "@shared/api/test-utils.js";

import "./admin-tabs.js";
import type { AdminTab, AdminTabs } from "./admin-tabs.js";

async function renderTabs(
  role: string | null,
  current: AdminTab = "blog",
): Promise<AdminTabs> {
  const tabs = document.createElement("admin-tabs") as AdminTabs;
  tabs.current = current;
  if (role !== null) {
    (tabs as any).session = {
      state: {
        status: "authenticated",
        message: "",
        user: { id: "u1", username: "U", role },
      },
      revalidate: () => {},
    };
  }
  document.body.appendChild(tabs);
  await tabs.updateComplete;
  return tabs;
}

function links(tabs: AdminTabs): HTMLAnchorElement[] {
  return Array.from(
    tabs.shadowRoot!.querySelectorAll("a"),
  ) as HTMLAnchorElement[];
}

afterEach(() => {
  document.body.innerHTML = "";
});

describe("admin-tabs", () => {
  test("every staff role sees the four shared tabs", async () => {
    for (const role of ["moderator", "admin", "super-admin"]) {
      document.body.innerHTML = "";
      const tabs = await renderTabs(role);
      const hrefs = links(tabs).map((a) => a.getAttribute("href"));

      expect(hrefs.slice(0, 4)).toEqual([
        "/admin",
        "/admin/projects",
        "/admin/users",
        "/admin/metrics",
      ]);
    }
  });

  test("below super-admin the logs tab is absent", async () => {
    for (const role of ["moderator", "admin"]) {
      document.body.innerHTML = "";
      const tabs = await renderTabs(role);
      const hrefs = links(tabs).map((a) => a.getAttribute("href"));

      expect(hrefs).not.toContain("/admin/logs");
      expect(hrefs.length).toBe(4);
    }
  });

  test("without a session the super-admin tab stays hidden", async () => {
    const tabs = await renderTabs(null);

    expect(links(tabs).length).toBe(4);
    expect(links(tabs).map((a) => a.getAttribute("href"))).not.toContain(
      "/admin/logs",
    );
  });

  test("a super-admin also sees the logs tab, labelled from the catalog", async () => {
    const tabs = await renderTabs("super-admin", "metrics");
    const logsLink = links(tabs).find(
      (a) => a.getAttribute("href") === "/admin/logs",
    );

    expect(links(tabs).length).toBe(5);
    expect(logsLink?.textContent?.trim()).toBe(el.admin.logsTab);
    expect(logsLink?.getAttribute("aria-current")).toBeNull();
  });

  test("the logs tab is current on its own page", async () => {
    const tabs = await renderTabs("super-admin", "logs");
    const current = links(tabs).filter(
      (a) => a.getAttribute("aria-current") === "page",
    );

    expect(current.length).toBe(1);
    expect(current[0]?.getAttribute("href")).toBe("/admin/logs");
  });

  test("the tab row scrolls instead of widening the page", async () => {
    // Four tabs (five for a super-admin) never fit a phone width, and the row's
    // min-content width propagated up to the route root — the owner's "tabs
    // overflow". The row scrolls (the account page's shape) and the tabs keep
    // their natural width; happy-dom resolves no CSS, so this is a source pin.
    const tabsStyles = stripCssComments(
      await Bun.file(new URL("./admin-tabs.css", import.meta.url)).text(),
    );
    const row = tabsStyles.match(/\.admin-tabs \{([^}]*)\}/s)?.[1] ?? "";
    expect(row).toContain("overflow-x: auto");
    expect(row).toContain("max-width: 100%");

    const tab = tabsStyles.match(/\.admin-tab \{([^}]*)\}/s)?.[1] ?? "";
    expect(tab).toContain("flex: 0 0 auto");
    expect(tab).toContain("white-space: nowrap");
  });
});
