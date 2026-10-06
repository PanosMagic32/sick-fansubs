// Sick-Fansubs service worker — offline caching for the PWA.
//
// Hand-written, no build step. The API server replaces the SF_VERSION
// placeholder with the deployed application version at serve time
// (cmd/api/frontend.go — the same mechanism as the index.html app-version
// meta tag), so every release ships a byte-different worker: browsers
// install it in the background, it waits for old tabs to close, and
// activate drops the previous release's asset cache. No skipWaiting and no
// clients.claim — the conservative update policy.
//
// Offline scope is read-only: the shell and previously visited pages render
// from cache. /api/v1 is network-only and is never cached, so no API data,
// session state, or staff content ever lands in a cache.
const SF_VERSION = "dev";

// Stable cache names survive deploys (offline pages and media stay useful
// across releases); the asset cache is version-keyed so activate can drop
// the previous release's orphaned hashed bundles.
const PAGES_CACHE = "sf-pages";
const ASSETS_CACHE = "sf-assets-" + SF_VERSION;
const MEDIA_CACHE = "sf-media";
const MEDIA_MAX_ENTRIES = 100;

// activate: remove asset caches from older versions. Pages and media are
// deliberate stable names — they are never wiped by an update. Cache
// contents are treated as disposable: Safari drops origin data
// after 7 inactive days and everything repopulates on demand.
self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(
        keys.map((key) => {
          if (key.startsWith("sf-assets-") && key !== ASSETS_CACHE) {
            return caches.delete(key);
          }
        }),
      ),
    ),
  );
});

// ── Web push ─────────────────────────────────────────────────────────
//
// The push payload carries DATA only:
// { kind, actorUsername, contentTitle, contentKind, contentId, commentId },
// where commentId is present for the comment kinds (comment_reply, heart,
// comment, comment_removed) and absent for content_updated, new_content,
// draft_activity, and the self-test send. The self-test carries EMPTY
// content fields — it has no content to name. The sentence templates below
// duplicate the Greek catalog values and are pinned equal by
// push-artifacts.test.ts (the static-duplicate precedent) — the
// worker cannot import the TS catalog.
const TPL_HEART_OPEN = "Το σχόλιό σου στο ";
const TPL_HEART_CLOSE = " αρέσει σε ";
const TPL_REPLY_VERB = "απάντησε στο σχόλιό σου στο";
const TPL_COMMENT_VERB = "σχολίασε στο";
const TPL_UPDATED_VERB = "ενημέρωσε το";
const TPL_NEW_VERB = "δημοσίευσε το";
// The staff-only draft_activity kind words the sentence by
// what happened to the unpublished draft, so it carries one verb per action
// rather than a single template.
const TPL_DRAFT_CREATED_VERB = "δημιούργησε το πρόχειρο";
const TPL_DRAFT_UPDATED_VERB = "ενημέρωσε το πρόχειρο";
const TPL_DRAFT_UNPUBLISHED_VERB = "απέσυρε το";
// comment_removed is the ACTORLESS sentence —
// the moderator is never named, so its template has no actor slot.
const TPL_REMOVED_OPEN = "Το σχόλιό σου στο ";
const TPL_REMOVED_CLOSE = " αφαιρέθηκε.";
// The self-test send: there is no content to
// title the notification with, so the worker supplies both lines — the
// title is the site name and the body states what the user just proved.
const TPL_TEST_TITLE = "Sick-Fansubs";
const TPL_TEST_BODY =
  "Οι ειδοποιήσεις λειτουργούν! Αυτή είναι μια δοκιμαστική ειδοποίηση.";

// draftVerb resolves the draft_activity verb. The wire pins
// created/updated/unpublished; an action this worker does not know yields
// null, and the caller then raises no notification at all — a wordless
// sentence would be worse than a missing one, and the feed applies the same
// refusal to the row.
function draftVerb(action) {
  switch (action) {
    case "created":
      return TPL_DRAFT_CREATED_VERB;
    case "updated":
      return TPL_DRAFT_UPDATED_VERB;
    case "unpublished":
      return TPL_DRAFT_UNPUBLISHED_VERB;
    default:
      return null;
  }
}

// deepLinkPath maps a wire contentKind to its frontend route: the
// comment kinds get the thread deep link
// (/blog/{contentId}#comment-{commentId} or /projects/…), the
// comment-less content_updated and new_content kinds get the content page
// itself (/blog/{contentId} or /projects/{contentId}). comment_removed
// carries the removed comment's id (the notification
// tag uses it) but its comment is GONE, so it navigates to the content
// page with NO fragment. draft_activity targets the
// STAFF editor (/admin/blog/{contentId} or /admin/projects/{contentId}) —
// its content is unpublished, so the public route has
// nothing to show. The self-test has no content at all — it lands on the
// settings that sent it. Malformed data → null.
function deepLinkPath(p) {
  if (!p) {
    return null;
  }
  if (p.kind === "test") {
    return "/account";
  }
  if (!p.contentKind || !p.contentId) {
    return null;
  }
  if (p.kind === "draft_activity") {
    return (
      (p.contentKind === "projects" ? "/admin/projects/" : "/admin/blog/") +
      p.contentId
    );
  }
  const segment = p.contentKind === "projects" ? "projects" : "blog";
  if (p.commentId && p.kind !== "comment_removed") {
    return "/" + segment + "/" + p.contentId + "#comment-" + p.commentId;
  }
  return "/" + segment + "/" + p.contentId;
}

// sentenceFor composes the kind's display line (notification
// title = the content title, body = the kind's sentence — the in-app
// forms). Malformed data → null (never show a broken notification): the
// kind and the title are always required, and every kind except the
// actorless comment_removed also requires the actor — a missing actor
// would render "… σε undefined". draft_activity additionally refuses an
// action it cannot word (draftVerb above). The self-test is the one kind
// with neither content nor actor: it renders its own fixed line.
function sentenceFor(p) {
  if (!p || !p.kind) {
    return null;
  }
  if (p.kind === "test") {
    return TPL_TEST_BODY;
  }
  if (!p.contentTitle) {
    return null;
  }
  if (p.kind === "comment_removed") {
    return TPL_REMOVED_OPEN + p.contentTitle + TPL_REMOVED_CLOSE;
  }
  if (!p.actorUsername) {
    return null;
  }
  switch (p.kind) {
    case "heart":
      return (
        TPL_HEART_OPEN + p.contentTitle + TPL_HEART_CLOSE + p.actorUsername
      );
    case "comment_reply":
      return p.actorUsername + " " + TPL_REPLY_VERB + " " + p.contentTitle;
    case "comment":
      return p.actorUsername + " " + TPL_COMMENT_VERB + " " + p.contentTitle;
    case "content_updated":
      return p.actorUsername + " " + TPL_UPDATED_VERB + " " + p.contentTitle;
    case "new_content":
      return p.actorUsername + " " + TPL_NEW_VERB + " " + p.contentTitle;
    case "draft_activity": {
      const verb = draftVerb(p.draftAction);
      if (verb === null) {
        return null;
      }
      return p.actorUsername + " " + verb + " " + p.contentTitle;
    }
    default:
      return null;
  }
}

self.addEventListener("push", (event) => {
  let data = null;
  try {
    data = event.data ? event.data.json() : null;
  } catch {
    return; // malformed payload — drop silently, the feed still has the row
  }
  const sentence = sentenceFor(data);
  const path = deepLinkPath(data);
  if (!sentence || !path) {
    return;
  }
  // The self-test has no content title to head the notification with.
  const title = data.kind === "test" ? TPL_TEST_TITLE : data.contentTitle;
  event.waitUntil(
    self.registration.showNotification(title, {
      body: sentence,
      icon: "/icons/icon-192.png",
      // Repeats (a re-heart, a burst on one comment, a second edit of the
      // same content, two test presses) replace instead of stacking;
      // the comment-less kinds fall back to the content id,
      // and the content-less self-test owns its own stable tag.
      tag:
        data.kind === "test"
          ? "sf-test"
          : "sf-" + data.kind + "-" + (data.commentId || data.contentId),
      data: { path },
    }),
  );
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const path = event.notification.data && event.notification.data.path;
  if (!path) {
    return;
  }
  const url = new URL(path, self.location.origin).href;
  event.waitUntil(
    self.clients
      .matchAll({ type: "window", includeUncontrolled: true })
      .then((clients) => {
        for (const client of clients) {
          if ("navigate" in client && "focus" in client) {
            return client
              .navigate(url)
              .then((c) => c.focus())
              .catch(() => self.clients.openWindow(url));
          }
        }
        return self.clients.openWindow(url);
      }),
  );
});

// pushsubscriptionchange: best-effort supplement, never the
// mechanism — tell open pages the push service rotated the subscription;
// the page-side module resubscribes (the CSRF path lives there).
self.addEventListener("pushsubscriptionchange", () => {
  self.clients.matchAll({ type: "window" }).then((clients) => {
    for (const client of clients) {
      client.postMessage({ type: "sf-push-subscription-change" });
    }
  });
});

self.addEventListener("fetch", (event) => {
  const request = event.request;
  if (request.method !== "GET") {
    return; // mutations and uploads pass through untouched
  }

  const url = new URL(request.url);
  if (url.origin !== self.location.origin) {
    return; // never intercept cross-origin traffic
  }
  if (url.pathname.startsWith("/api/v1/")) {
    return; // network-only: API responses are never cached
  }

  if (request.mode === "navigate") {
    networkFirstHTML(event);
    return;
  }
  if (url.pathname.startsWith("/assets/")) {
    event.respondWith(cacheFirst(request, ASSETS_CACHE));
    return;
  }
  if (url.pathname.startsWith("/media/images/")) {
    event.respondWith(mediaFirstCapped(request));
    return;
  }
  // Everything else (manifest, icons, fonts, favicon): browser default.
});

// Network-first HTML: serve the network response and stash it per-URL so
// previously visited pages render offline; when the network fails, fall
// back to the cached copy of the same URL, then to the cached shell ("/").
// If even the shell is missing (never visited since the worker installed),
// the final fetch(request) rethrows and the browser shows its network
// error page — an honest state, not a broken one. Only text/html responses
// are stashed, so navigations to non-HTML endpoints (e.g. /health/live)
// never pollute the page cache.
function networkFirstHTML(event) {
  const request = event.request;
  event.respondWith(
    fetch(request)
      .then((response) => {
        if (
          response.ok &&
          response.headers.get("Content-Type")?.startsWith("text/html")
        ) {
          // Stash without blocking the navigation.
          caches
            .open(PAGES_CACHE)
            .then((cache) =>
              event.waitUntil(cache.put(request, response.clone())),
            );
        }
        return response;
      })
      .catch(async () => {
        const cache = await caches.open(PAGES_CACHE);
        return (
          (await cache.match(request)) ||
          (await cache.match("/")) ||
          fetch(request)
        );
      }),
  );
}

// Cache-first for immutable content: Vite-hashed /assets/* and
// content-addressed /media/images/* (the URL is a hash of the bytes, so a
// cache hit can never serve wrong data). Every miss populates the cache as
// the response passes through — runtime cache-on-response.
async function cacheFirst(request, cacheName) {
  const cache = await caches.open(cacheName);
  const hit = await cache.match(request);
  if (hit) {
    return hit;
  }
  const response = await fetch(request);
  if (response.ok) {
    cache.put(request, response.clone());
  }
  return response;
}

// Media cache with a bounded entry count: on each miss-cache, trim the
// oldest entry past the cap. Cache.keys() returns insertion order and
// cache.match() does not reorder, so keys[0] is the oldest-INSERTED entry
// (FIFO eviction — good enough for content-addressed media). A concurrent
// write can overshoot the cap briefly; the next miss trims it — a cosmetic
// race, not a leak.
async function mediaFirstCapped(request) {
  const cache = await caches.open(MEDIA_CACHE);
  const hit = await cache.match(request);
  if (hit) {
    return hit;
  }
  const response = await fetch(request);
  if (response.ok) {
    cache.put(request, response.clone());
    const keys = await cache.keys();
    if (keys.length > MEDIA_MAX_ENTRIES) {
      await cache.delete(keys[0]);
    }
  }
  return response;
}
