/**
 * Global push lifecycle tests — the marker gate, the ownership backfill for
 * pre-existing subscribers, the server-side heal, the lost-subscription
 * prompt, and the wake-up/relay triggers. Browser APIs are stubbed per test
 * and restored LIFO; the fetch double speaks the push-subscriptions wire.
 */

import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";

import {
  mockFetchImpl,
  mockResponse,
  mockSessionContext,
  readSheet,
  restoreMockFetch,
  stubProperty,
  waitFor,
} from "@shared/api/test-utils.js";
import { el as catalog } from "@shared/catalog/el.js";

import "./push-lifecycle.js";
import type { PushLifecycle } from "./push-lifecycle.js";
import {
  PUSH_OWNER_KEY,
  PUSH_RECONCILED_EVENT,
  PUSH_SUBSCRIPTION_CHANGE_MESSAGE,
  readPushOwner,
  rememberPushOwner,
  type StoredPushSubscription,
} from "../data-access/push-subscription.js";

interface ServerRow {
  id: string;
  endpoint: string;
  label?: string | null;
  createdAt: string;
}

interface StubOptions {
  permission?: "default" | "granted" | "denied";
  /** The local subscription's endpoint (absent = none registered locally). */
  localEndpoint?: string;
  /** The endpoint `pushManager.subscribe` creates. */
  newEndpoint?: string;
  /** The rows the list endpoint serves (mutable by the double). */
  serverRows?: ServerRow[];
  /** The result of the browser permission prompt. */
  requestPermission?: "default" | "granted" | "denied";
  /** Set true to make the service-worker probe find nothing. */
  noRegistration?: boolean;
  /** Set true to make the list GET fail. */
  failList?: boolean;
}

interface FetchCall {
  input: RequestInfo | URL;
  init?: RequestInit;
}

const restoreStubs: Array<() => void> = [];
let fetchCalls: FetchCall[] = [];
let container: HTMLElement;
const lifecycleStyles = await readSheet(
  new URL("./push-lifecycle.css", import.meta.url),
);

/** Rows announced through `sf-push-reconciled`; reset per test. */
let reconciled: StoredPushSubscription[] = [];
let onReconciled: ((event: Event) => void) | null = null;

function stubSubscription(endpoint: string) {
  return {
    endpoint,
    toJSON: () => ({
      endpoint,
      expirationTime: null,
      keys: { p256dh: "cHVibGljLWtleQ", auth: "YXV0aC1zZWNyZXQ" },
    }),
    unsubscribe: mock(async () => true),
  };
}

function stubPushEnvironment(o: StubOptions = {}) {
  const local = o.localEndpoint ? stubSubscription(o.localEndpoint) : null;
  const subscribeFn = mock(async () =>
    stubSubscription(o.newEndpoint ?? "https://push.example.com/new"),
  );
  const registration = {
    pushManager: {
      subscribe: subscribeFn,
      getSubscription: mock(async () => local),
    },
  };
  const requestPermissionFn = mock(
    async () => o.requestPermission ?? "granted",
  );

  restoreStubs.push(
    stubProperty(globalThis, "PushManager", class {}),
    stubProperty(navigator, "serviceWorker", {
      ready: Promise.resolve(registration),
      getRegistration: mock(async () =>
        o.noRegistration ? null : registration,
      ),
    }),
    stubProperty(globalThis, "Notification", {
      permission: o.permission ?? "granted",
      requestPermission: requestPermissionFn,
    }),
  );

  const rows = [...(o.serverRows ?? [])];
  mockFetchImpl(async (input: RequestInfo | URL, init?: RequestInit) => {
    fetchCalls.push({ input, init });
    const url = typeof input === "string" ? input : input.toString();
    const method = init?.method ?? "GET";
    if (method === "GET" && url.includes("/push-subscriptions")) {
      if (o.failList) {
        return mockResponse({
          ok: false,
          status: 500,
          headers: { "Content-Type": "application/problem+json" },
          jsonBody: { type: "/problems/internal-error", title: "boom" },
        });
      }
      return mockResponse({
        ok: true,
        status: 200,
        jsonBody: { subscriptions: rows },
      });
    }
    if (method === "POST" && url.includes("/push-subscriptions")) {
      const body = JSON.parse(String(init?.body ?? "null")) as {
        endpoint: string;
      };
      const row = {
        id: "s-new",
        endpoint: body.endpoint,
        label: null,
        createdAt: "2026-09-13T12:00:00.000Z",
      };
      rows.push(row);
      return mockResponse({ ok: true, status: 201, jsonBody: { id: row.id } });
    }
    return mockResponse({ ok: false, status: 404, jsonBody: {} });
  });

  return { subscribeFn, requestPermissionFn };
}

function lists(): FetchCall[] {
  return fetchCalls.filter(
    ({ input, init }) =>
      (init?.method ?? "GET") === "GET" &&
      String(input).includes("/push-subscriptions"),
  );
}

function posts(): FetchCall[] {
  return fetchCalls.filter(
    ({ input, init }) =>
      init?.method === "POST" && String(input).includes("/push-subscriptions"),
  );
}

async function settle(el: PushLifecycle) {
  await el.updateComplete;
  await new Promise((resolve) => setTimeout(resolve, 0));
  await el.updateComplete;
}

function root(el: PushLifecycle): HTMLElement {
  return el.shadowRoot! as unknown as HTMLElement;
}

function mount(): PushLifecycle {
  container = document.createElement("div");
  // The shell's shadow root holds `main`, so the prompt's focus handoff has
  // a real target instead of silently skipping the document root.
  const shell = container.attachShadow({ mode: "open" });
  const main = document.createElement("main");
  main.tabIndex = -1;
  shell.appendChild(main);
  const el = document.createElement("push-lifecycle") as PushLifecycle;
  shell.appendChild(el);
  document.body.appendChild(container);
  return el;
}

function shellMain(): HTMLElement | null {
  return container.shadowRoot?.querySelector("main") ?? null;
}

async function mountAuthenticated(): Promise<PushLifecycle> {
  const el = mount();
  (el as unknown as { session: unknown }).session = mockSessionContext();
  await settle(el);
  return el;
}

function capturedReconciles(): StoredPushSubscription[] {
  return reconciled;
}

beforeEach(() => {
  fetchCalls = [];
  reconciled = [];
  onReconciled = (event) => {
    reconciled.push((event as CustomEvent<StoredPushSubscription>).detail);
  };
  window.addEventListener(PUSH_RECONCILED_EVENT, onReconciled);
  localStorage.removeItem(PUSH_OWNER_KEY);
});

afterEach(() => {
  container?.remove();
  if (onReconciled) {
    window.removeEventListener(PUSH_RECONCILED_EVENT, onReconciled);
    onReconciled = null;
  }
  for (let i = restoreStubs.length - 1; i >= 0; i--) restoreStubs[i]!();
  restoreStubs.length = 0;
  restoreMockFetch();
  localStorage.removeItem(PUSH_OWNER_KEY);
});

describe("push-lifecycle", () => {
  test("does nothing without an authenticated session", async () => {
    stubPushEnvironment();
    const el = mount();
    await settle(el);

    expect(fetchCalls).toHaveLength(0);
    expect(root(el).querySelector(".push-heal")).toBeNull();
  });

  test("adopts a pre-existing subscriber whose server row matches this device", async () => {
    const endpoint = "https://push.example.com/current";
    stubPushEnvironment({
      localEndpoint: endpoint,
      serverRows: [
        { id: "s1", endpoint, createdAt: "2026-09-13T12:00:00.000Z" },
      ],
    });
    const el = await mountAuthenticated();
    await waitFor(() => lists().length === 1);
    await settle(el);

    // The old subscriber predates the marker; the server row is the proof
    // that this account already opted in on this device.
    expect(readPushOwner()).toBe("u1");
    expect(posts()).toHaveLength(0);
    expect(root(el).querySelector(".push-heal")).toBeNull();
  });

  test("never heals a device owned by another account", async () => {
    rememberPushOwner("u2");
    stubPushEnvironment({
      localEndpoint: "https://push.example.com/current",
      serverRows: [],
    });
    const el = await mountAuthenticated();
    await settle(el);

    expect(lists()).toHaveLength(0);
    expect(posts()).toHaveLength(0);
    expect(readPushOwner()).toBe("u2");
    expect(root(el).querySelector(".push-heal")).toBeNull();
  });

  test("does not adopt an unmarked device when the server row is absent", async () => {
    stubPushEnvironment({
      localEndpoint: "https://push.example.com/current",
      serverRows: [
        {
          id: "s1",
          endpoint: "https://push.example.com/other",
          createdAt: "2026-09-13T12:00:00.000Z",
        },
      ],
    });
    const el = await mountAuthenticated();
    await waitFor(() => lists().length === 1);
    await settle(el);

    expect(readPushOwner()).toBeNull();
    expect(posts()).toHaveLength(0);
    expect(root(el).querySelector(".push-heal")).toBeNull();
  });

  test("heals a rotated subscription whose server row is missing", async () => {
    const endpoint = "https://push.example.com/current";
    rememberPushOwner("u1");
    stubPushEnvironment({ localEndpoint: endpoint, serverRows: [] });
    const el = await mountAuthenticated();
    await waitFor(() => posts().length === 1);
    await settle(el);

    expect(JSON.parse(String(posts()[0]?.init?.body ?? "null"))).toEqual({
      endpoint,
      keys: { p256dh: "cHVibGljLWtleQ", auth: "YXV0aC1zZWNyZXQ" },
    });
    expect(capturedReconciles()).toEqual([{ id: "s-new", endpoint }]);
    expect(root(el).querySelector(".push-heal")).toBeNull();
  });

  test("prompts instead of silently recreating a lost subscription", async () => {
    rememberPushOwner("u1");
    const { subscribeFn } = stubPushEnvironment({
      localEndpoint: undefined,
      permission: "granted",
    });
    const el = await mountAuthenticated();
    await settle(el);

    const prompt = root(el).querySelector(".push-heal");
    expect(prompt).not.toBeNull();
    expect(prompt?.getAttribute("aria-label")).toBe(
      catalog.notifications.pushTitle,
    );
    const copy = prompt?.querySelector(".push-heal-copy");
    expect(copy?.textContent?.trim()).toBe(catalog.notifications.pushHealBody);
    // The persistent live region is the accessible copy; the visible copy is
    // hidden from the accessibility tree so a virtual cursor reads it once.
    expect(copy?.getAttribute("aria-hidden")).toBe("true");
    expect(root(el).querySelector('[role="status"]')?.textContent?.trim()).toBe(
      catalog.notifications.pushHealBody,
    );
    expect(posts()).toHaveLength(0);
    expect(subscribeFn).not.toHaveBeenCalled();
  });

  test("mounts the live region before its text changes", async () => {
    rememberPushOwner("u1");
    stubPushEnvironment({ localEndpoint: undefined });
    const el = mount();
    (el as unknown as { session: unknown }).session = mockSessionContext();
    await el.updateComplete;

    const live = root(el).querySelector<HTMLElement>('[role="status"]');
    expect(live).not.toBeNull();
    expect(live?.textContent?.trim()).toBe("");

    await settle(el);
    expect(root(el).querySelector('[role="status"]')).toBe(live);
    expect(live?.textContent?.trim()).toBe(catalog.notifications.pushHealBody);
  });

  test("does not prompt when notification permission is denied", async () => {
    rememberPushOwner("u1");
    stubPushEnvironment({ localEndpoint: undefined, permission: "denied" });
    const el = await mountAuthenticated();
    await settle(el);

    expect(root(el).querySelector(".push-heal")).toBeNull();
    expect(posts()).toHaveLength(0);
    // The blocked outcome is still announced, with the browser-settings hint.
    expect(root(el).querySelector('[role="status"]')?.textContent?.trim()).toBe(
      catalog.notifications.pushDenied,
    );
  });

  test("a denied enable press suppresses the prompt", async () => {
    rememberPushOwner("u1");
    stubPushEnvironment({
      localEndpoint: undefined,
      permission: "default",
      requestPermission: "denied",
    });
    const el = await mountAuthenticated();
    await settle(el);

    root(el)
      .querySelector<HTMLButtonElement>(".push-heal-actions button")!
      .click();
    await settle(el);

    expect(root(el).querySelector(".push-heal")).toBeNull();
    expect(posts()).toHaveLength(0);
    expect(root(el).querySelector('[role="status"]')?.textContent?.trim()).toBe(
      catalog.notifications.pushDenied,
    );
    expect(container.shadowRoot?.activeElement).toBe(shellMain());
  });

  test("dismissing the prompt clears the owner marker", async () => {
    rememberPushOwner("u1");
    stubPushEnvironment({ localEndpoint: undefined });
    const el = await mountAuthenticated();
    await settle(el);

    const buttons = root(el).querySelectorAll<HTMLButtonElement>(
      ".push-heal-actions button",
    );
    buttons[1]!.click();
    await settle(el);

    expect(root(el).querySelector(".push-heal")).toBeNull();
    expect(readPushOwner()).toBeNull();
    expect(posts()).toHaveLength(0);
    expect(container.shadowRoot?.activeElement).toBe(shellMain());
  });

  test("a reconcile event clears a focused prompt with a focus handoff", async () => {
    rememberPushOwner("u1");
    stubPushEnvironment({ localEndpoint: undefined });
    const el = await mountAuthenticated();
    await settle(el);

    const enable = root(el).querySelector<HTMLButtonElement>(
      ".push-heal-actions button",
    )!;
    enable.focus();
    expect(el.shadowRoot?.activeElement).toBe(enable);

    window.dispatchEvent(
      new CustomEvent(PUSH_RECONCILED_EVENT, {
        detail: { id: "s1", endpoint: "https://push.example.com/current" },
      }),
    );
    await settle(el);

    expect(root(el).querySelector(".push-heal")).toBeNull();
    expect(container.shadowRoot?.activeElement).toBe(shellMain());
  });

  test("the prompt's enable press subscribes and stores the row", async () => {
    rememberPushOwner("u1");
    const { subscribeFn, requestPermissionFn } = stubPushEnvironment({
      localEndpoint: undefined,
      newEndpoint: "https://push.example.com/new",
    });
    const el = await mountAuthenticated();
    await settle(el);

    root(el)
      .querySelector<HTMLButtonElement>(".push-heal-actions button")!
      .click();
    await waitFor(() => posts().length === 1);
    await settle(el);

    expect(requestPermissionFn).toHaveBeenCalledTimes(1);
    expect(subscribeFn).toHaveBeenCalledTimes(1);
    expect(JSON.parse(String(posts()[0]?.init?.body ?? "null"))).toEqual({
      endpoint: "https://push.example.com/new",
      keys: { p256dh: "cHVibGljLWtleQ", auth: "YXV0aC1zZWNyZXQ" },
      // The prompt's enable press is an explicit opt-in.
      takeover: true,
    });
    expect(readPushOwner()).toBe("u1");
    expect(root(el).querySelector(".push-heal")).toBeNull();
    expect(capturedReconciles()).toEqual([
      { id: "s-new", endpoint: "https://push.example.com/new" },
    ]);
    expect(container.shadowRoot?.activeElement).toBe(shellMain());
  });

  test("marks the prompt controls busy while the enable press is in flight", async () => {
    rememberPushOwner("u1");
    const { requestPermissionFn } = stubPushEnvironment({
      localEndpoint: undefined,
    });
    let release: (value: "granted" | "denied" | "default") => void = () => {};
    requestPermissionFn.mockImplementation(
      () => new Promise((resolve) => (release = resolve)),
    );
    const el = await mountAuthenticated();
    await settle(el);

    const enable = root(el).querySelector<HTMLButtonElement>(
      ".push-heal-actions button",
    )!;
    enable.click();
    await settle(el);

    expect(enable.getAttribute("aria-disabled")).toBe("true");
    expect(enable.textContent?.trim()).toBe(
      catalog.notifications.pushEnablePending,
    );

    release("granted");
    await waitFor(() => posts().length === 1);
    await settle(el);
  });

  test("a session switch during the permission prompt aborts the takeover", async () => {
    rememberPushOwner("u1");
    const { requestPermissionFn, subscribeFn } = stubPushEnvironment({
      localEndpoint: undefined,
    });
    let release: (value: "granted" | "denied" | "default") => void = () => {};
    requestPermissionFn.mockImplementation(
      () => new Promise((resolve) => (release = resolve)),
    );
    const el = mount();
    const session = mockSessionContext();
    (el as unknown as { session: unknown }).session = session;
    await settle(el);

    root(el)
      .querySelector<HTMLButtonElement>(".push-heal-actions button")!
      .click();
    await settle(el);

    // Another tab signs in as a different account while the browser prompt
    // is still open.
    session.state = {
      status: "authenticated",
      message: "",
      user: { id: "u2", username: "Other", role: "user" },
    };
    release("granted");
    await settle(el);

    // The explicit takeover must not run under the new account, and no
    // stale owner marker is written.
    expect(subscribeFn).not.toHaveBeenCalled();
    expect(posts()).toHaveLength(0);
    expect(readPushOwner()).toBe("u1");
    expect(root(el).querySelector(".push-heal")).toBeNull();
  });

  test("reconciles on online and visibilitychange", async () => {
    const endpoint = "https://push.example.com/current";
    rememberPushOwner("u1");
    restoreStubs.push(stubProperty(document, "visibilityState", "visible"));
    stubPushEnvironment({
      localEndpoint: endpoint,
      serverRows: [
        { id: "s1", endpoint, createdAt: "2026-09-13T12:00:00.000Z" },
      ],
    });
    await mountAuthenticated();
    await waitFor(() => lists().length === 1);

    window.dispatchEvent(new Event("online"));
    await waitFor(() => lists().length === 2);

    document.dispatchEvent(new Event("visibilitychange"));
    await waitFor(() => lists().length === 3);
  });

  test("online reconciles even while the tab is hidden", async () => {
    const endpoint = "https://push.example.com/current";
    rememberPushOwner("u1");
    restoreStubs.push(stubProperty(document, "visibilityState", "hidden"));
    stubPushEnvironment({
      localEndpoint: endpoint,
      serverRows: [
        { id: "s1", endpoint, createdAt: "2026-09-13T12:00:00.000Z" },
      ],
    });
    await mountAuthenticated();
    await waitFor(() => lists().length === 1);

    window.dispatchEvent(new Event("online"));
    await waitFor(() => lists().length === 2);
  });

  test("pins the healthy host and the phone-band action row", () => {
    expect(lifecycleStyles).toMatch(
      /:host\(\[data-live-only\]\) \{\s*display: contents;/,
    );
    const phone = lifecycleStyles.match(
      /@media \(max-width: 640px\) \{([\s\S]*?)\n\}/,
    )?.[1];
    // Extract each rule body: a declaration from a sibling selector must not
    // satisfy the pin for the action row.
    const actionRow =
      phone?.match(/\.push-heal-actions \{([^}]*)\}/)?.[1] ?? "";
    expect(actionRow).toContain("width: 100%");
    const actionButtons =
      phone?.match(/\.push-heal-actions \.button \{([^}]*)\}/)?.[1] ?? "";
    expect(actionButtons).toContain("flex: 1 1 auto");
    // The pending controls keep full contrast instead of inheriting the
    // shared aria-disabled opacity.
    const pending =
      lifecycleStyles.match(
        /\.push-heal-actions \.button\[aria-disabled="true"\] \{([^}]*)\}/,
      )?.[1] ?? "";
    expect(pending).toContain("opacity: 1");
  });

  test("owns the rotation relay and ignores foreign origins", async () => {
    const endpoint = "https://push.example.com/current";
    rememberPushOwner("u1");
    stubPushEnvironment({
      localEndpoint: endpoint,
      serverRows: [
        { id: "s1", endpoint, createdAt: "2026-09-13T12:00:00.000Z" },
      ],
    });
    const el = await mountAuthenticated();
    await waitFor(() => lists().length === 1);

    window.dispatchEvent(
      new MessageEvent("message", {
        origin: "https://evil.example",
        data: { type: PUSH_SUBSCRIPTION_CHANGE_MESSAGE },
      }),
    );
    await settle(el);
    expect(lists()).toHaveLength(1);

    window.dispatchEvent(
      new MessageEvent("message", {
        origin: location.origin,
        data: { type: PUSH_SUBSCRIPTION_CHANGE_MESSAGE },
      }),
    );
    await waitFor(() => lists().length === 2);
  });

  test("collapses wake-ups that arrive during a run into one follow-up", async () => {
    const endpoint = "https://push.example.com/current";
    rememberPushOwner("u1");
    stubPushEnvironment({
      localEndpoint: endpoint,
      serverRows: [
        { id: "s1", endpoint, createdAt: "2026-09-13T12:00:00.000Z" },
      ],
    });

    let release: (() => void) | null = null;
    let first = true;
    mockFetchImpl(async (input: RequestInfo | URL, init?: RequestInit) => {
      fetchCalls.push({ input, init });
      if (first) {
        first = false;
        await new Promise<void>((resolve) => {
          release = resolve;
        });
      }
      return mockResponse({
        ok: true,
        status: 200,
        jsonBody: {
          subscriptions: [
            { id: "s1", endpoint, createdAt: "2026-09-13T12:00:00.000Z" },
          ],
        },
      });
    });

    const el = await mountAuthenticated();
    await waitFor(() => lists().length === 1);
    window.dispatchEvent(
      new MessageEvent("message", {
        origin: location.origin,
        data: { type: PUSH_SUBSCRIPTION_CHANGE_MESSAGE },
      }),
    );
    window.dispatchEvent(
      new MessageEvent("message", {
        origin: location.origin,
        data: { type: PUSH_SUBSCRIPTION_CHANGE_MESSAGE },
      }),
    );
    release!();
    await waitFor(() => lists().length === 2);
    await settle(el);

    expect(lists()).toHaveLength(2);
  });

  test("does nothing when the environment cannot push", async () => {
    rememberPushOwner("u1");
    mockFetchImpl(async () => {
      throw new Error("must not fetch");
    });
    const el = await mountAuthenticated();
    await settle(el);

    expect(root(el).querySelector(".push-heal")).toBeNull();
    expect(fetchCalls).toHaveLength(0);
  });
});
