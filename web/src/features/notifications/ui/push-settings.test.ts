/**
 * Push settings section tests: the visibility rules (anonymous HIDES the
 * section; an unavailable environment STATES it), the double-permission
 * subscribe flow (dialog → browser prompt → subscribe → store), the denied
 * state, the off switch, the per-kind toggles, the failure + retry path,
 * and the pushsubscriptionchange resubscribe relay.
 *
 * Browser APIs (serviceWorker/PushManager/Notification) are stubbed per
 * test through `stubProperty` — happy-dom provides none of them — and every
 * stub is restored in afterEach (one window is shared per bun worker).
 */

import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";

import { el as catalog } from "@shared/catalog/el.js";
import {
  mockFetchImpl,
  mockResponse,
  mockSessionContext,
  restoreMockFetch,
  stripCssComments,
  stripSourceComments,
  stubProperty,
  waitFor,
} from "@shared/api/test-utils.js";

import "@features/auth/data-access/session.js";
import "./push-settings.js";
import { type PushSettings } from "./push-settings.js";

function mockSession(
  status: "authenticated" | "anonymous" = "authenticated",
  role = "user",
) {
  return mockSessionContext({ status, user: { role } });
}

/** A stubbed PushManager subscription with a BROWSER-FAITHFUL toJSON()
 * (the spec's PushSubscriptionJSON includes expirationTime — the strict
 * create endpoint must never see it; the component destructures). */
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

interface StubOptions {
  permission?: "default" | "granted" | "denied";
  /** The local subscription's endpoint ("" = none registered locally). */
  localEndpoint?: string;
  /** Rows the server list returns (each {id, endpoint, createdAt}). */
  serverRows?: { id: string; endpoint: string; createdAt: string }[];
  /** The preferences payload. */
  prefs?: { kind: string; pushEnabled: boolean }[];
  /** Set true to make the load GETs fail (the error/retry path). */
  failLoad?: boolean;
  /** The self-test response's delivered count (default 1). */
  testDelivered?: number;
  /** One outcome per attempted endpoint. */
  testEndpoints?: {
    id: string;
    service: string;
    accepted: boolean;
    status?: number;
    removed?: boolean;
  }[];
  /** Rows the subscription list serves AFTER a test press — models the
   * dead-endpoint cleanup that removes the row during the send. */
  serverRowsAfterTest?: { id: string; endpoint: string; createdAt: string }[];
  /** Set true to make the self-test POST fail (the mapped-error path). */
  failTest?: boolean;
  /** Preference kinds whose PUT must fail (the save-fan-out failure path). */
  failPrefPutFor?: string[];
  /** Set true to make the capability probe find no service-worker
   * registration (the unavailable line with `supported` still true). */
  noRegistration?: boolean;
}

/** Every request the active fetch double saw (method-filtered helpers
 * below). Reset per test. */
let fetchCalls: Array<[RequestInfo | URL, RequestInit | undefined]> = [];

/** Per-test stub restores, run LIFO in afterEach (a second stub of the same
 * property must restore before the first). */
const restoreStubs: Array<() => void> = [];

function stubPushEnvironment(o: StubOptions) {
  const local = o.localEndpoint ? stubSubscription(o.localEndpoint) : null;
  const subscribeFn = mock(async () =>
    stubSubscription("https://push.example.com/new"),
  );

  const registration = {
    pushManager: {
      subscribe: subscribeFn,
      getSubscription: mock(async () => local),
    },
  };

  restoreStubs.push(
    stubProperty(globalThis, "PushManager", class {}),
    stubProperty(navigator, "serviceWorker", {
      ready: Promise.resolve(registration),
      getRegistration: mock(async () =>
        o.noRegistration ? null : registration,
      ),
    }),
    stubProperty(globalThis, "Notification", {
      permission: o.permission ?? "default",
      // The BROWSER PROMPT's result — the flow always models a successful
      // prompt here; the refused-permission test overrides this stub after
      // stubbing.
      requestPermission: mock(async () => "granted"),
    }),
  );

  let pressed = false;
  mockFetchImpl(async (input: RequestInfo | URL, init?: RequestInit) => {
    fetchCalls.push([input, init]);
    const url = typeof input === "string" ? input : input.toString();
    const method = init?.method ?? "GET";
    if (o.failLoad && method === "GET") {
      return mockResponse({
        ok: false,
        status: 500,
        headers: { "Content-Type": "application/problem+json" },
        jsonBody: { type: "/problems/internal-error", title: "boom" },
      });
    }
    if (method === "GET" && url.includes("/push-subscriptions")) {
      const rows =
        pressed && o.serverRowsAfterTest
          ? o.serverRowsAfterTest
          : (o.serverRows ?? []);
      return mockResponse({
        ok: true,
        status: 200,
        jsonBody: { subscriptions: rows },
      });
    }
    if (method === "GET" && url.includes("/notification-preferences")) {
      return mockResponse({
        ok: true,
        status: 200,
        jsonBody: { preferences: o.prefs ?? [] },
      });
    }
    if (method === "POST" && url.includes("/push-subscriptions/test")) {
      pressed = true;
      if (o.failTest) {
        return mockResponse({
          ok: false,
          status: 500,
          headers: { "Content-Type": "application/problem+json" },
          jsonBody: { type: "/problems/internal-error", title: "boom" },
        });
      }
      const delivered = o.testDelivered ?? 1;
      return mockResponse({
        ok: true,
        status: 200,
        // The default report is INTERNALLY consistent — one accepted row per
        // delivered endpoint, the shape the real sender produces — so a test
        // cannot pin a state the server never emits (the Go routes double's
        // rule). `testEndpoints` overrides it for the refusal paths.
        jsonBody: {
          delivered,
          endpoints:
            o.testEndpoints ??
            Array.from({ length: delivered }, (_, i) => ({
              id: i === 0 ? "s1" : `s${i + 1}`,
              service: "mozilla",
              accepted: true,
              status: 201,
            })),
        },
      });
    }
    if (method === "POST" && url.includes("/push-subscriptions")) {
      return mockResponse({
        ok: true,
        status: 201,
        jsonBody: { id: "s-new" },
      });
    }
    if (method === "DELETE") {
      return mockResponse({ ok: true, status: 204, jsonBody: null });
    }
    if (method === "PUT") {
      if (o.failPrefPutFor?.some((kind) => url.endsWith(`/${kind}`)) === true) {
        return mockResponse({
          ok: false,
          status: 500,
          headers: { "Content-Type": "application/problem+json" },
          jsonBody: { type: "/problems/internal-error", title: "boom" },
        });
      }
      return mockResponse({ ok: true, status: 204, jsonBody: null });
    }
    return mockResponse({ ok: false, status: 404, jsonBody: {} });
  });

  return { subscribeFn };
}

let container: HTMLElement;

function mount(): PushSettings {
  container = document.createElement("div");
  document.body.appendChild(container);
  const el = document.createElement("push-settings");
  container.appendChild(el);
  return el;
}

function root(el: PushSettings): HTMLElement {
  return el.shadowRoot! as unknown as HTMLElement;
}

async function settle(el: PushSettings) {
  await el.updateComplete;
  await new Promise((r) => setTimeout(r, 0));
  await el.updateComplete;
}

beforeEach(() => {
  fetchCalls = [];
});

afterEach(() => {
  container?.remove();
  // LIFO: a second stub of the same property must restore before the first.
  for (let i = restoreStubs.length - 1; i >= 0; i--) restoreStubs[i]!();
  restoreStubs.length = 0;
  restoreMockFetch();
});

/** All requests of one method the active fetch double saw. */
function requests(
  method: string,
): Array<[RequestInfo | URL, RequestInit | undefined]> {
  return fetchCalls.filter(([, init]) => init?.method === method);
}

/** Every preference PUT the component issued (the save fan-out). */
function preferencePuts(): Array<[RequestInfo | URL, RequestInit | undefined]> {
  return fetchCalls.filter(
    ([input, init]) =>
      init?.method === "PUT" &&
      typeof input === "string" &&
      input.includes("/notification-preferences/"),
  );
}

/** The stylesheet is read from disk and comment-stripped: contract pins must
 * never pass or fail on prose (testing rule 7). */
const pushStyles = stripCssComments(
  await Bun.file(new URL("./push-settings.css", import.meta.url)).text(),
);

function text(el: PushSettings, selector: string): string | null {
  const node = root(el).querySelector(selector);
  return node?.textContent?.trim() ?? null;
}

/** The stub options for a section that renders SUBSCRIBED on this device
 * (the shared setup of every self-test press below). */
function subscribedOptions(extra: Partial<StubOptions> = {}): StubOptions {
  const endpoint = "https://push.example.com/current";
  return {
    permission: "granted",
    localEndpoint: endpoint,
    serverRows: [{ id: "s1", endpoint, createdAt: "2026-09-13T12:00:00.000Z" }],
    ...extra,
  };
}

/** Mount + attach an authenticated session + settle. */
async function mountAuthenticated(): Promise<PushSettings> {
  const el = mount();
  (el as unknown as { session: unknown }).session = mockSession();
  await settle(el);
  return el;
}

/** The test-press requests the component issued. */
function testPressCalls(): Array<[RequestInfo | URL, RequestInit | undefined]> {
  return fetchCalls.filter(
    ([input, init]) =>
      init?.method === "POST" &&
      typeof input === "string" &&
      input.includes("/push-subscriptions/test"),
  );
}

/** Every subscription-list GET: the initial load plus one re-read per test
 * press (the refresh is every-press, not zero-only). */
function subscriptionGets(): Array<
  [RequestInfo | URL, RequestInit | undefined]
> {
  return fetchCalls.filter(
    ([input, init]) =>
      (init?.method ?? "GET") === "GET" &&
      typeof input === "string" &&
      input.includes("/push-subscriptions"),
  );
}

/** Every preferences GET — the load's second leg. */
function preferenceGets(): Array<[RequestInfo | URL, RequestInit | undefined]> {
  return fetchCalls.filter(
    ([input, init]) =>
      (init?.method ?? "GET") === "GET" &&
      typeof input === "string" &&
      input.includes("/notification-preferences"),
  );
}

describe("push-settings", () => {
  test("renders nothing for anonymous sessions", async () => {
    stubPushEnvironment({});
    const el = mount();
    (el as unknown as { session: unknown }).session = mockSession("anonymous");
    await settle(el);
    expect(root(el).querySelector(".push-settings")).toBeNull();
  });

  test("states the unavailable line without ServiceWorker/PushManager", async () => {
    // No ServiceWorker (the SW registers production-only) and no PushManager
    // → the section states the state instead of vanishing: it has its own
    // account tab, and a blank tab reads as a broken page. No fetch double
    // is installed: any request here would be a defect.
    mockFetchImpl(async () => {
      throw new Error("must not fetch");
    });
    const el = mount();
    (el as unknown as { session: unknown }).session = mockSession();
    await settle(el);

    const section = root(el).querySelector(".push-settings");
    expect(section).not.toBeNull();
    expect(
      section?.querySelector(".push-unavailable")?.textContent?.trim(),
    ).toBe(catalog.notifications.pushUnavailable);
    // No control set: nothing to press in an environment that cannot push.
    expect(root(el).querySelector(".push-actions")).toBeNull();
    expect(root(el).querySelector(".push-kinds")).toBeNull();

    // A same-origin relay message must not throw in an environment without
    // the Notification API (the listener reads it only behind a guard).
    window.dispatchEvent(
      new MessageEvent("message", {
        origin: location.origin,
        data: { type: "sf-push-subscription-change" },
      }),
    );
    await settle(el);
    expect(text(el, ".push-unavailable")).toBe(
      catalog.notifications.pushUnavailable,
    );
  });

  test("states the unavailable line when no service-worker registration exists", async () => {
    // The capability APIs exist, but the registration probe finds none (the
    // dev-server shape): the section must state the line, never hang on
    // `serviceWorker.ready` or offer a dead enable flow.
    stubPushEnvironment({ noRegistration: true });
    const el = mount();
    (el as unknown as { session: unknown }).session = mockSession();
    await settle(el);

    expect(text(el, ".push-unavailable")).toBe(
      catalog.notifications.pushUnavailable,
    );
    expect(root(el).querySelector(".push-actions")).toBeNull();
  });

  test("shows the enable button and runs the double-permission flow", async () => {
    const { subscribeFn } = stubPushEnvironment({ permission: "default" });
    const el = mount();
    (el as unknown as { session: unknown }).session = mockSession();
    await settle(el);

    // No subscription → the enable button.
    const enable = root(el).querySelector(".push-on") as HTMLButtonElement;
    expect(enable).not.toBeNull();
    expect(text(el, ".push-on")).toBe(catalog.notifications.pushEnable);

    // Click → OUR dialog first (the double-permission pattern) — the
    // browser prompt must NOT fire yet.
    enable.click();
    await settle(el);
    expect(root(el).querySelector("dialog")).not.toBeNull();
    expect(Notification.requestPermission).not.toHaveBeenCalled();

    // Confirm → browser prompt → subscribe → store (the browser's
    // expirationTime field must NOT reach the strict create endpoint —
    // the destructure pin).
    (
      root(el).querySelector(".push-dialog-actions button") as HTMLButtonElement
    ).click();
    await settle(el);
    expect(Notification.requestPermission).toHaveBeenCalledTimes(1);
    expect(subscribeFn).toHaveBeenCalledTimes(1);
    expect(
      (subscribeFn.mock.calls[0] as unknown[] | undefined)?.[0] as unknown,
    ).toEqual({
      userVisibleOnly: true,
      applicationServerKey: expect.any(ArrayBuffer),
    });
    const posts = requests("POST");
    expect(posts).toHaveLength(1);
    const posted = JSON.parse((posts[0]?.[1]?.body as string) ?? "null");
    expect(Object.keys(posted).sort()).toEqual(["endpoint", "keys"]);
    expect(Object.keys(posted.keys).sort()).toEqual(["auth", "p256dh"]);
    // The enable button is gone (off switch now).
    expect(root(el).querySelector(".push-on")).toBeNull();
    expect(root(el).querySelector(".push-off")).not.toBeNull();
  });

  test("renders the denied hint instead of the enable button", async () => {
    stubPushEnvironment({ permission: "denied" });
    const el = mount();
    (el as unknown as { session: unknown }).session = mockSession();
    await settle(el);
    expect(text(el, ".push-denied")).toBe(catalog.notifications.pushDenied);
    expect(root(el).querySelector(".push-on")).toBeNull();
  });

  test("off switch deletes the server row and unsubscribes locally", async () => {
    const endpoint = "https://push.example.com/current";
    const { subscribeFn } = stubPushEnvironment({
      permission: "granted",
      localEndpoint: endpoint,
      serverRows: [
        { id: "s1", endpoint, createdAt: "2026-09-13T12:00:00.000Z" },
      ],
    });
    const el = mount();
    (el as unknown as { session: unknown }).session = mockSession();
    await settle(el);

    expect(root(el).querySelector(".push-off")).not.toBeNull();
    (root(el).querySelector(".push-off") as HTMLButtonElement).click();
    await settle(el);

    const deleteCalls = requests("DELETE");
    expect(deleteCalls).toHaveLength(1);
    expect(deleteCalls[0]?.[0] as string).toContain("/push-subscriptions/s1");
    // The local registration was unsubscribed.
    expect(subscribeFn).not.toHaveBeenCalled();
    // Back to the enable state.
    expect(root(el).querySelector(".push-off")).toBeNull();
    expect(root(el).querySelector(".push-on")).not.toBeNull();
  });

  test("the self-test button presses the channel and reports this device's acceptance", async () => {
    stubPushEnvironment(
      subscribedOptions({
        testDelivered: 2,
        testEndpoints: [
          { id: "s1", service: "fcm", accepted: true, status: 201 },
          { id: "s9", service: "apple", accepted: true, status: 201 },
        ],
      }),
    );
    const el = await mountAuthenticated();

    const button = root(el).querySelector(".push-test") as HTMLButtonElement;
    expect(button.textContent?.trim()).toBe(
      catalog.notifications.pushTestButton,
    );
    expect(text(el, ".push-test-result")).toBe("");

    button.click();
    await settle(el);

    expect(testPressCalls()).toHaveLength(1);
    expect(text(el, ".push-test-result")).toBe(
      catalog.notifications.pushTestSent,
    );
    // EVERY press re-reads this device's row (not only a zero): the send may
    // have removed it, and a subscribed state surviving its own row would
    // offer a dead switch.
    expect(subscriptionGets()).toHaveLength(2);
  });

  test("a press the push service refused for THIS device says so, however healthy the sibling devices are", async () => {
    stubPushEnvironment(
      subscribedOptions({
        testDelivered: 1, // a sibling accepted — the count alone must not speak for this device
        testEndpoints: [
          { id: "s1", service: "mozilla", accepted: false, status: 413 },
          { id: "s9", service: "fcm", accepted: true, status: 201 },
        ],
      }),
    );
    const el = await mountAuthenticated();

    (root(el).querySelector(".push-test") as HTMLButtonElement).click();
    await settle(el);

    expect(text(el, ".push-test-result")).toBe(
      catalog.notifications.pushTestDeviceRejected,
    );
    // The row survived a rejection: the switch is still offered.
    expect(root(el).querySelector(".push-off")).not.toBeNull();
  });

  test("a press that never reached the push service says so, not 'refused'", async () => {
    stubPushEnvironment(
      subscribedOptions({
        testDelivered: 0,
        testEndpoints: [
          // No status: no attempt ever got a response (the transport failed).
          { id: "s1", service: "mozilla", accepted: false },
        ],
      }),
    );
    const el = await mountAuthenticated();

    (root(el).querySelector(".push-test") as HTMLButtonElement).click();
    await settle(el);

    expect(text(el, ".push-test-result")).toBe(
      catalog.notifications.pushTestDeviceUnreachable,
    );
  });

  test("a press that removed this device's row says so and drops the subscribed state", async () => {
    stubPushEnvironment(
      subscribedOptions({
        testDelivered: 0,
        testEndpoints: [
          {
            id: "s1",
            service: "mozilla",
            accepted: false,
            status: 410,
            removed: true,
          },
        ],
        // The send deleted the row: the refresh must no longer see it.
        serverRowsAfterTest: [],
      }),
    );
    const el = await mountAuthenticated();

    (root(el).querySelector(".push-test") as HTMLButtonElement).click();
    await settle(el);

    expect(text(el, ".push-test-result")).toBe(
      catalog.notifications.pushTestDeviceRemoved,
    );
    // The refresh found the row gone, so the enable flow is back.
    expect(root(el).querySelector(".push-test")).toBeNull();
    expect(root(el).querySelector(".push-on")).not.toBeNull();
  });

  test("a press that reached nothing and named no device of mine says so, and re-reads the device list", async () => {
    stubPushEnvironment(subscribedOptions({ testDelivered: 0 }));
    const el = await mountAuthenticated();

    (root(el).querySelector(".push-test") as HTMLButtonElement).click();
    await settle(el);

    expect(text(el, ".push-test-result")).toBe(
      catalog.notifications.pushTestNone,
    );
    // The press re-read this device's row: exactly the two initial loads
    // (subscriptions + preferences) plus the one re-read of the rows.
    expect(subscriptionGets()).toHaveLength(2);
  });

  test("a failed test press maps to the catalog error, with no result line", async () => {
    stubPushEnvironment(subscribedOptions({ failTest: true }));
    const el = await mountAuthenticated();

    (root(el).querySelector(".push-test") as HTMLButtonElement).click();
    await settle(el);

    // The stub answers a known problem type, so the EXACT mapped copy is
    // the assertion — a raw problem path would fail this.
    expect(text(el, ".push-error")).toBe(
      catalog.problems["/problems/internal-error"],
    );
    expect(text(el, ".push-test-result")).toBe("");
  });

  test("the test button exists only while this device is subscribed, and a switch-off clears the stale result", async () => {
    stubPushEnvironment(subscribedOptions({ testDelivered: 1 }));
    const el = await mountAuthenticated();

    (root(el).querySelector(".push-test") as HTMLButtonElement).click();
    await settle(el);
    expect(text(el, ".push-test-result")).toBe(
      catalog.notifications.pushTestSent,
    );

    // Switch off, then back on: the result must not come back with the
    // button (the branch alone would hide it, so re-enabling is the actual
    // assertion).
    (root(el).querySelector(".push-off") as HTMLButtonElement).click();
    await settle(el);
    expect(root(el).querySelector(".push-test")).toBeNull();

    (root(el).querySelector(".push-on") as HTMLButtonElement).click();
    await settle(el);
    (
      root(el).querySelector(".push-dialog-actions button") as HTMLButtonElement
    ).click();
    await settle(el);
    expect(root(el).querySelector(".push-test")).not.toBeNull();
    expect(text(el, ".push-test-result")).toBe("");
  });

  test("the kind form is a draft until save — only the changed kinds are PUT", async () => {
    const endpoint = "https://push.example.com/current";
    stubPushEnvironment({
      permission: "granted",
      localEndpoint: endpoint,
      serverRows: [
        { id: "s1", endpoint, createdAt: "2026-09-13T12:00:00.000Z" },
      ],
      prefs: [
        { kind: "heart", pushEnabled: true },
        { kind: "comment_reply", pushEnabled: false },
      ],
    });
    const el = mount();
    (el as unknown as { session: unknown }).session = mockSession();
    await settle(el);

    const checkboxes =
      root(el).querySelectorAll<HTMLInputElement>(".push-kind input");
    expect(checkboxes).toHaveLength(2);
    expect(checkboxes[0]?.checked).toBe(true);
    expect(checkboxes[1]?.checked).toBe(false);

    // A form that matches the loaded preferences offers no save control.
    expect(root(el).querySelector(".push-save")).toBeNull();

    const reply = checkboxes[1]!;
    reply.checked = true;
    reply.dispatchEvent(new Event("change"));
    await settle(el);

    // A toggle edit writes NOTHING on its own.
    expect(preferencePuts()).toHaveLength(0);

    const save = root(el).querySelector<HTMLButtonElement>(".push-save");
    expect(save?.textContent?.trim()).toBe(catalog.notifications.pushSave);
    save!.click();
    await settle(el);

    const puts = preferencePuts();
    expect(puts).toHaveLength(1);
    expect(puts[0]?.[0] as string).toContain(
      "/notification-preferences/comment_reply",
    );
    expect(JSON.parse((puts[0]?.[1]?.body as string) ?? "null")).toEqual({
      pushEnabled: true,
    });

    // Committed → the form is clean and the control is gone again.
    expect(root(el).querySelector(".push-save")).toBeNull();
  });

  test("flipping a toggle back to its loaded value leaves nothing to save", async () => {
    const endpoint = "https://push.example.com/current";
    stubPushEnvironment({
      permission: "granted",
      localEndpoint: endpoint,
      serverRows: [
        { id: "s1", endpoint, createdAt: "2026-09-13T12:00:00.000Z" },
      ],
      prefs: [{ kind: "heart", pushEnabled: true }],
    });
    const el = mount();
    (el as unknown as { session: unknown }).session = mockSession();
    await settle(el);

    const heart = root(el).querySelector<HTMLInputElement>(".push-kind input");
    heart!.checked = false;
    heart!.dispatchEvent(new Event("change"));
    await settle(el);
    expect(root(el).querySelector(".push-save")).not.toBeNull();

    // Flipped back: the draft equals the loaded value, so the control goes
    // and no request can be produced from the excursion.
    heart!.checked = true;
    heart!.dispatchEvent(new Event("change"));
    await settle(el);
    expect(root(el).querySelector(".push-save")).toBeNull();
    expect(preferencePuts()).toHaveLength(0);
  });

  test("a failed kind keeps its draft, reports the mapped error, and retry sends only the leftovers", async () => {
    const endpoint = "https://push.example.com/current";
    stubPushEnvironment({
      permission: "granted",
      localEndpoint: endpoint,
      serverRows: [
        { id: "s1", endpoint, createdAt: "2026-09-13T12:00:00.000Z" },
      ],
      prefs: [
        { kind: "heart", pushEnabled: true },
        { kind: "comment_reply", pushEnabled: false },
      ],
      failPrefPutFor: ["comment_reply"],
    });
    const el = mount();
    (el as unknown as { session: unknown }).session = mockSession();
    await settle(el);

    const checkboxes =
      root(el).querySelectorAll<HTMLInputElement>(".push-kind input");
    checkboxes[0]!.checked = false; // heart: will succeed
    checkboxes[0]!.dispatchEvent(new Event("change"));
    checkboxes[1]!.checked = true; // comment_reply: will fail
    checkboxes[1]!.dispatchEvent(new Event("change"));
    await settle(el);

    root(el).querySelector<HTMLButtonElement>(".push-save")!.click();
    await settle(el);

    // Both edited kinds were attempted once…
    expect(preferencePuts()).toHaveLength(2);
    // …the saved one committed, the failed one kept its draft.
    expect(
      root(el).querySelector<HTMLInputElement>(".push-kind input")?.checked,
    ).toBe(false);
    expect(
      root(el).querySelectorAll<HTMLInputElement>(".push-kind input")[1]
        ?.checked,
    ).toBe(true);
    expect(text(el, ".push-error")).toBe(
      catalog.problems["/problems/internal-error"],
    );
    expect(root(el).querySelector(".push-save")).not.toBeNull();

    // The retry re-sends exactly the leftover — never the committed kind.
    const before = preferencePuts().length;
    root(el).querySelector<HTMLButtonElement>(".push-save")!.click();
    await settle(el);
    const retried = preferencePuts().slice(before);
    expect(retried).toHaveLength(1);
    expect(retried[0]?.[0] as string).toContain(
      "/notification-preferences/comment_reply",
    );
  });

  test("the self-test result is ONE live region — the line can never render twice", async () => {
    // With a result set, the sentence must render (and be announced) exactly
    // once.
    stubPushEnvironment(subscribedOptions({ testDelivered: 1 }));
    const el = await mountAuthenticated();
    expect(root(el).querySelectorAll(".push-test-result")).toHaveLength(1);
  });

  test("the section's controls are styled by function (primary / secondary / danger)", async () => {
    // happy-dom cannot compute styles: the vocabulary is pinned at its
    // source — the markup classes — plus the one local rule (the off switch
    // warms to the error colour under the pointer).
    const source = stripSourceComments(
      await Bun.file(new URL("./push-settings.ts", import.meta.url)).text(),
    );
    expect(source).toContain('class="button button--primary push-on"');
    expect(source).toContain('class="button button--primary push-save"');
    expect(source).toContain('class="button button--secondary push-test"');
    expect(source).toContain('class="button button--secondary push-retry"');
    expect(source).toContain('class="button button--secondary push-off"');
    expect(pushStyles).toMatch(
      /\.push-actions \.push-off:hover:not\(:disabled\) \{[^}]*--sf-error/s,
    );

    // Phone band: the section's controls own their row.
    const phone =
      pushStyles.match(/@media \(max-width: 700px\) \{(.*?)\n\}/s)?.[1] ?? "";
    expect(phone).toContain("flex: 1 0 100%");
  });

  test("the kind toggles are sized for comfortable tapping", () => {
    // happy-dom cannot compute styles; the geometry is pinned against the
    // raw sheet.
    const kind = pushStyles.match(/\.push-kind \{([^}]*)\}/s)?.[1] ?? "";
    expect(kind).toContain("min-height: 44px");
    expect(kind).toContain("gap: 0.75rem");
    const checkbox =
      pushStyles.match(
        /\.push-kind input\[type="checkbox"\] \{([^}]*)\}/s,
      )?.[1] ?? "";
    expect(checkbox).toContain("width: 1.25rem");
    expect(checkbox).toContain("height: 1.25rem");
    // The hover tint must be a mix of the text token — --sf-surface is
    // near-identical to the card fill it would composite over (no-op).
    const hover = pushStyles.match(/\.push-kind:hover \{([^}]*)\}/s)?.[1] ?? "";
    expect(hover).toContain(
      "color-mix(in srgb, var(--sf-text) 7%, transparent)",
    );
    expect(hover).not.toContain("--sf-surface");
  });

  test("the six kind toggles render with the six catalog labels", async () => {
    const endpoint = "https://push.example.com/current";
    stubPushEnvironment({
      permission: "granted",
      localEndpoint: endpoint,
      serverRows: [
        { id: "s1", endpoint, createdAt: "2026-09-13T12:00:00.000Z" },
      ],
      prefs: [
        { kind: "heart", pushEnabled: true },
        { kind: "comment_reply", pushEnabled: false },
        { kind: "comment", pushEnabled: true },
        { kind: "content_updated", pushEnabled: false },
        { kind: "new_content", pushEnabled: true },
        { kind: "comment_removed", pushEnabled: false },
      ],
    });
    const el = mount();
    (el as unknown as { session: unknown }).session = mockSession();
    await settle(el);

    const labels = root(el).querySelectorAll(".push-kind span");
    expect(labels).toHaveLength(6);
    expect(labels[0]?.textContent?.trim()).toBe(
      catalog.notifications.pushKindHeart,
    );
    expect(labels[1]?.textContent?.trim()).toBe(
      catalog.notifications.pushKindReply,
    );
    expect(labels[2]?.textContent?.trim()).toBe(
      catalog.notifications.pushKindComment,
    );
    expect(labels[3]?.textContent?.trim()).toBe(
      catalog.notifications.pushKindUpdated,
    );
    expect(labels[4]?.textContent?.trim()).toBe(
      catalog.notifications.pushKindNewContent,
    );
    expect(labels[5]?.textContent?.trim()).toBe(
      catalog.notifications.pushKindRemoved,
    );
  });

  test("the staff-only draft_activity toggle saves the kind", async () => {
    const endpoint = "https://push.example.com/current";
    stubPushEnvironment({
      permission: "granted",
      localEndpoint: endpoint,
      serverRows: [
        { id: "s1", endpoint, createdAt: "2026-09-21T12:00:00.000Z" },
      ],
      // A moderator session, because the server omits the kind below that
      // floor — and the section itself never role-checks: it renders the
      // kinds its list carried.
      prefs: [{ kind: "draft_activity", pushEnabled: false }],
    });
    const el = mount();
    (el as unknown as { session: unknown }).session = mockSession(
      "authenticated",
      "moderator",
    );
    await settle(el);

    const labels = root(el).querySelectorAll(".push-kind span");
    expect(labels).toHaveLength(1);
    expect(labels[0]?.textContent?.trim()).toBe(
      catalog.notifications.pushKindDraft,
    );

    const toggle = root(el).querySelector<HTMLInputElement>(".push-kind input");
    expect(toggle?.checked).toBe(false);
    toggle!.checked = true;
    toggle!.dispatchEvent(new Event("change"));
    await settle(el);

    // The edit rides the form's one save control.
    root(el).querySelector<HTMLButtonElement>(".push-save")!.click();
    await settle(el);

    const puts = preferencePuts();
    expect(puts).toHaveLength(1);
    expect(puts[0]?.[0] as string).toContain(
      "/notification-preferences/draft_activity",
    );
    expect(JSON.parse((puts[0]?.[1]?.body as string) ?? "null")).toEqual({
      pushEnabled: true,
    });
  });

  test("shows the failure line when the permission is refused", async () => {
    stubPushEnvironment({ permission: "default" });
    // Override the permission result AFTER stubbing: refuse it (the LIFO
    // restore undoes this stub before the environment's).
    restoreStubs.push(
      stubProperty(globalThis, "Notification", {
        permission: "default",
        requestPermission: mock(async () => "denied"),
      }),
    );
    const el = mount();
    (el as unknown as { session: unknown }).session = mockSession();
    await settle(el);

    (root(el).querySelector(".push-on") as HTMLButtonElement).click();
    await settle(el);
    (
      root(el).querySelector(".push-dialog-actions button") as HTMLButtonElement
    ).click();
    await settle(el);

    expect(text(el, ".push-error")).toBe(catalog.notifications.pushFailure);
    expect(text(el, ".push-denied")).toBe(catalog.notifications.pushDenied);
  });

  test("a failed load shows the error and retry refetches exactly once", async () => {
    stubPushEnvironment({ failLoad: true });
    const el = mount();
    (el as unknown as { session: unknown }).session = mockSession();
    await settle(el);

    expect(root(el).querySelector(".push-error")).not.toBeNull();
    expect(root(el).querySelector(".push-retry")).not.toBeNull();

    // Re-stub the GETs as healthy, then retry. Re-arming the latch and letting
    // its update load is ONE fetch pair — calling load() beside it would fire
    // an aborted pair first (the NS_BINDING_ABORTED double-fetch).
    stubPushEnvironment({});
    fetchCalls = [];
    (root(el).querySelector(".push-retry") as HTMLButtonElement).click();
    await waitFor(() => preferenceGets().length > 0);
    await settle(el);
    expect(root(el).querySelector(".push-error")).toBeNull();
    expect(root(el).querySelector(".push-on")).not.toBeNull();
    expect(subscriptionGets()).toHaveLength(1);
    expect(preferenceGets()).toHaveLength(1);
  });

  test("resubscribes when the worker relays pushsubscriptionchange", async () => {
    const endpoint = "https://push.example.com/current";
    stubPushEnvironment({
      permission: "granted",
      localEndpoint: endpoint,
      serverRows: [
        { id: "s1", endpoint, createdAt: "2026-09-13T12:00:00.000Z" },
      ],
    });
    const el = mount();
    (el as unknown as { session: unknown }).session = mockSession();
    await settle(el);

    window.dispatchEvent(
      new MessageEvent("message", {
        origin: location.origin,
        data: { type: "sf-push-subscription-change" },
      }),
    );
    await settle(el);

    const posts = requests("POST");
    expect(posts).toHaveLength(1);
    expect(posts[0]?.[0] as string).toContain("/push-subscriptions");
  });

  test("relay resubscribes even when the rotated local subscription left no server match", async () => {
    // After a push-service rotation the local registration is GONE — the
    // endpoint match yields nothing and the enable button shows. The
    // relay must still heal the subscription (the rotation scenario).
    stubPushEnvironment({ permission: "granted" });
    const el = mount();
    (el as unknown as { session: unknown }).session = mockSession();
    await settle(el);
    expect(root(el).querySelector(".push-on")).not.toBeNull();

    window.dispatchEvent(
      new MessageEvent("message", {
        origin: location.origin,
        data: { type: "sf-push-subscription-change" },
      }),
    );
    await settle(el);

    const posts = requests("POST");
    expect(posts).toHaveLength(1);
    // The fresh row landed — the off switch replaced the enable button.
    expect(root(el).querySelector(".push-off")).not.toBeNull();
  });

  test("ignores the relay message from a foreign origin", async () => {
    stubPushEnvironment({ permission: "granted" });
    const el = mount();
    (el as unknown as { session: unknown }).session = mockSession();
    await settle(el);

    window.dispatchEvent(
      new MessageEvent("message", {
        origin: "https://evil.example",
        data: { type: "sf-push-subscription-change" },
      }),
    );
    await settle(el);

    const posts = requests("POST");
    expect(posts).toHaveLength(0);
  });

  test("states the iOS install hint in a browser session on iOS", async () => {
    restoreStubs.push(
      stubProperty(
        navigator,
        "userAgent",
        "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15",
      ),
      stubProperty(window, "matchMedia", ((query: string) => ({
        matches: false,
        media: query,
      })) as unknown as typeof window.matchMedia),
    );
    stubPushEnvironment({});
    const el = await mountAuthenticated();

    expect(text(el, ".push-hint")).toBe(catalog.notifications.pushIOSHint);
  });

  test("hides the iOS install hint once the app is installed", async () => {
    restoreStubs.push(
      stubProperty(
        navigator,
        "userAgent",
        "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15",
      ),
      stubProperty(window, "matchMedia", ((query: string) => ({
        matches: true,
        media: query,
      })) as unknown as typeof window.matchMedia),
    );
    stubPushEnvironment({});
    const el = await mountAuthenticated();

    // The ready state without the hint: the enable flow proves the section
    // loaded (a failed load would also render no hint).
    expect(root(el).querySelector(".push-on")).not.toBeNull();
    expect(root(el).querySelector(".push-hint")).toBeNull();
  });

  test("the confirm dialog falls back to the open attribute without showModal", async () => {
    restoreStubs.push(
      stubProperty(HTMLDialogElement.prototype, "showModal", undefined),
    );
    stubPushEnvironment({ permission: "default" });
    const el = mount();
    (el as unknown as { session: unknown }).session = mockSession();
    await settle(el);

    (root(el).querySelector(".push-on") as HTMLButtonElement).click();
    await settle(el);

    const dialog = root(el).querySelector("dialog")!;
    expect(dialog.hasAttribute("open")).toBe(true);
    expect(Notification.requestPermission).not.toHaveBeenCalled();
  });

  test("cancelling the confirm dialog closes it without prompting", async () => {
    stubPushEnvironment({ permission: "default" });
    const el = mount();
    (el as unknown as { session: unknown }).session = mockSession();
    await settle(el);

    (root(el).querySelector(".push-on") as HTMLButtonElement).click();
    await settle(el);
    const dialog = root(el).querySelector("dialog")!;
    // The second action is the cancel control.
    (
      root(el).querySelectorAll(
        ".push-dialog-actions button",
      )[1] as HTMLButtonElement
    ).click();
    await settle(el);

    expect(dialog.hasAttribute("open")).toBe(false);
    expect(Notification.requestPermission).not.toHaveBeenCalled();
  });

  test("a test press in flight gates the off/test/save controls; the relay stays quiet", async () => {
    const endpoint = "https://push.example.com/current";
    stubPushEnvironment(
      subscribedOptions({
        prefs: [{ kind: "heart", pushEnabled: true }],
      }),
    );
    // Replace the environment's fetch with one that holds the test POST open
    // (the browser stubs stay).
    let resolveTest: ((response: Response) => void) | null = null;
    mockFetchImpl(async (input: RequestInfo | URL, init?: RequestInit) => {
      fetchCalls.push([input, init]);
      const url = typeof input === "string" ? input : input.toString();
      const method = init?.method ?? "GET";
      if (method === "POST" && url.includes("/push-subscriptions/test")) {
        return new Promise<Response>((resolve) => {
          resolveTest = resolve;
        });
      }
      if (method === "GET" && url.includes("/push-subscriptions")) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: {
            subscriptions: [
              { id: "s1", endpoint, createdAt: "2026-09-13T12:00:00.000Z" },
            ],
          },
        });
      }
      if (method === "GET" && url.includes("/notification-preferences")) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: { preferences: [{ kind: "heart", pushEnabled: true }] },
        });
      }
      return mockResponse({ ok: false, status: 404, jsonBody: {} });
    });

    const el = await mountAuthenticated();

    // A dirty draft mounts the save control.
    const checkbox =
      root(el).querySelector<HTMLInputElement>(".push-kind input")!;
    checkbox.checked = false;
    checkbox.dispatchEvent(new Event("change"));
    await settle(el);
    expect(root(el).querySelector(".push-save")).not.toBeNull();

    (root(el).querySelector(".push-test") as HTMLButtonElement).click();
    await waitFor(() => testPressCalls().length === 1);
    await el.updateComplete;

    expect(
      (root(el).querySelector(".push-off") as HTMLButtonElement).disabled,
    ).toBe(true);
    expect(
      (root(el).querySelector(".push-test") as HTMLButtonElement).disabled,
    ).toBe(true);
    expect(
      (root(el).querySelector(".push-save") as HTMLButtonElement).disabled,
    ).toBe(true);
    // The toggle stays editable: a draft edit never touches the wire, and the
    // save control is the one that is gated.
    expect(
      root(el).querySelector<HTMLInputElement>(".push-kind input")!.disabled,
    ).toBe(false);

    // The worker relay must not slip a resubscribe past the in-flight press.
    window.dispatchEvent(
      new MessageEvent("message", {
        origin: location.origin,
        data: { type: "sf-push-subscription-change" },
      }),
    );
    await settle(el);
    expect(requests("POST")).toHaveLength(1);

    resolveTest!(
      mockResponse({
        ok: true,
        status: 200,
        jsonBody: {
          delivered: 1,
          endpoints: [
            { id: "s1", service: "mozilla", accepted: true, status: 201 },
          ],
        },
      }),
    );
    await settle(el);
    expect(root(el).querySelector(".push-test")).not.toBeNull();
  });

  test("the relay ignores a message while the permission is denied", async () => {
    stubPushEnvironment({ permission: "denied" });
    const el = mount();
    (el as unknown as { session: unknown }).session = mockSession();
    await settle(el);
    // The denied state proves the section loaded (not a failed-load blank).
    expect(text(el, ".push-denied")).toBe(catalog.notifications.pushDenied);

    window.dispatchEvent(
      new MessageEvent("message", {
        origin: location.origin,
        data: { type: "sf-push-subscription-change" },
      }),
    );
    await settle(el);

    expect(requests("POST")).toHaveLength(0);
  });

  test("two relay messages in quick succession issue one resubscribe", async () => {
    stubPushEnvironment({ permission: "granted" });
    const el = mount();
    (el as unknown as { session: unknown }).session = mockSession();
    await settle(el);
    expect(root(el).querySelector(".push-on")).not.toBeNull();

    // The first message marks the relay in flight synchronously, so the
    // second must not start a parallel subscribe.
    const relay = () =>
      window.dispatchEvent(
        new MessageEvent("message", {
          origin: location.origin,
          data: { type: "sf-push-subscription-change" },
        }),
      );
    relay();
    relay();
    await settle(el);

    expect(requests("POST")).toHaveLength(1);
    expect(root(el).querySelector(".push-off")).not.toBeNull();
  });
});
