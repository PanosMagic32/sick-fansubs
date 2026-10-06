/**
 * Push service-worker pins.
 *
 * sw.js cannot import the TS catalog, so the Greek sentence templates are
 * duplicated there — these tests read the REAL web/public/sw.js and pin
 * the duplicates EQUAL to the catalog values: drift fails the test
 * instead of shipping a notification in stale copy. The handler set and
 * the conservative shapes (icon/tag/dedupe, the pushsubscriptionchange
 * relay) are pinned the same way the PWA artifact pins work.
 */

import { describe, expect, test } from "bun:test";

import { el } from "@shared/catalog/el.js";

async function readSw(): Promise<string> {
  return Bun.file(new URL("../../public/sw.js", import.meta.url)).text();
}

function quoted(value: string): string {
  return `"${value}"`;
}

describe("push service worker", () => {
  test("carries the push, notificationclick, and pushsubscriptionchange handlers", async () => {
    const sw = await readSw();
    expect(sw).toContain('self.addEventListener("push"');
    expect(sw).toContain('self.addEventListener("notificationclick"');
    expect(sw).toContain('self.addEventListener("pushsubscriptionchange"');
  });

  test("sentence templates equal the catalog values", async () => {
    const sw = await readSw();
    // The eight template literals must be the EXACT catalog strings; the
    // removed pair is pinned at its ASSIGNMENT (verbCommentRemovedOpen is
    // the same text as verbHeartOpen, so a bare contains would pass even if
    // TPL_REMOVED_OPEN were deleted).
    expect(sw).toContain(quoted(el.notifications.verbHeartOpen));
    expect(sw).toContain(quoted(el.notifications.verbHeartClose));
    expect(sw).toContain(quoted(el.notifications.verbCommentReply));
    expect(sw).toContain(quoted(el.notifications.verbComment));
    expect(sw).toContain(quoted(el.notifications.verbContentUpdated));
    expect(sw).toContain(quoted(el.notifications.verbNewContent));
    expect(sw).toContain(
      "const TPL_REMOVED_OPEN = " +
        quoted(el.notifications.verbCommentRemovedOpen),
    );
    expect(sw).toContain(
      "const TPL_REMOVED_CLOSE = " +
        quoted(el.notifications.verbCommentRemovedClose),
    );
  });

  test("the self-test templates match the catalog and land on the settings", async () => {
    const sw = await readSw();
    // The notification title is the site name (the payload has no content
    // title) and the body is the catalog sentence — both pinned equal, the
    // body at its ASSIGNMENT so an unused constant cannot satisfy the pin.
    expect(sw).toContain("const TPL_TEST_TITLE = " + quoted(el.ui.siteName));
    // The body is pinned at its ASSIGNMENT (an unused constant cannot
    // satisfy this): the long Greek literal wraps to the next line, so the
    // value is looked up inside the assignment's neighbourhood.
    const bodyAssign = sw.indexOf("const TPL_TEST_BODY =");
    expect(bodyAssign).toBeGreaterThan(-1);
    expect(sw.slice(bodyAssign, bodyAssign + 200)).toContain(
      quoted(el.notifications.pushTestNotification),
    );
    expect(sw).toContain('return "/account";');
    // …and the deep link's branch order too: that function guards on
    // contentKind/contentId right after, and the test payload has neither.
    const deepLink = sw.slice(
      sw.indexOf("function deepLinkPath"),
      sw.indexOf("function sentenceFor"),
    );
    expect(deepLink.indexOf('if (p.kind === "test")')).toBeLessThan(
      deepLink.indexOf("if (!p.contentKind || !p.contentId)"),
    );
    // Whitespace-tolerant: the pin is the tag's SEMANTICS, not prettier's
    // line breaking.
    expect(sw).toMatch(/tag:\s*data\.kind === "test"\s*\?\s*"sf-test"/s);

    // The test payload carries NO content and NO actor, so the branch must
    // run before the contentTitle guard INSIDE sentenceFor — a reorder
    // would drop the notification while the API still reported a delivery.
    // Scoped to the function body on purpose: the deep-link branch carries
    // the same condition earlier in the file and would satisfy a bare
    // indexOf (the vacuous-pin class this suite has hit before).
    const sentenceFor = sw.slice(
      sw.indexOf("function sentenceFor"),
      sw.indexOf('self.addEventListener("push"'),
    );
    const testBranch = sentenceFor.indexOf('if (p.kind === "test")');
    const titleGuard = sentenceFor.indexOf("if (!p.contentTitle)");
    expect(testBranch).toBeGreaterThan(-1);
    expect(titleGuard).toBeGreaterThan(-1);
    expect(testBranch).toBeLessThan(titleGuard);
    // …and the handler must actually USE both template halves.
    expect(sentenceFor).toContain("return TPL_TEST_BODY;");
    expect(sw).toContain(
      'data.kind === "test" ? TPL_TEST_TITLE : data.contentTitle',
    );
    expect(sw).toContain("showNotification(title, {");
  });

  test("comment_removed composes the actorless sentence and skips the fragment", async () => {
    const sw = await readSw();
    // The actorless branch runs BEFORE the actor guard, so a payload with
    // no actorUsername still renders.
    expect(sw).toContain('if (p.kind === "comment_removed")');
    expect(sw).toContain(
      "return TPL_REMOVED_OPEN + p.contentTitle + TPL_REMOVED_CLOSE;",
    );
    // Its comment is gone: the deep link never carries the fragment, while
    // the payload's commentId still feeds the tag.
    expect(sw).toContain('if (p.commentId && p.kind !== "comment_removed")');
  });

  test("draft_activity words the sentence per action and links the staff editor", async () => {
    const sw = await readSw();
    // The three verbs are pinned at their ASSIGNMENTS: TPL_DRAFT_UPDATED_VERB
    // repeats verbContentUpdated's text, so a bare contains would pass even
    // with the constant missing.
    expect(sw).toContain(
      "const TPL_DRAFT_CREATED_VERB = " +
        quoted(el.notifications.verbDraftCreated),
    );
    expect(sw).toContain(
      "const TPL_DRAFT_UPDATED_VERB = " +
        quoted(el.notifications.verbDraftUpdated),
    );
    expect(sw).toContain(
      "const TPL_DRAFT_UNPUBLISHED_VERB = " +
        quoted(el.notifications.verbDraftUnpublished),
    );

    // The lookup refuses any other action — no notification beats a
    // wordless one (the feed drops the row the same way).
    const draftVerb = sw.slice(
      sw.indexOf("function draftVerb"),
      sw.indexOf("function deepLinkPath"),
    );
    expect(draftVerb).toContain("return TPL_DRAFT_CREATED_VERB;");
    expect(draftVerb).toContain("return TPL_DRAFT_UPDATED_VERB;");
    expect(draftVerb).toContain("return TPL_DRAFT_UNPUBLISHED_VERB;");
    expect(draftVerb).toMatch(/default:\s*return null;/s);

    // The sentence is the actor + verb + title shape, guarded so the refusal
    // reaches the notification gate. Scoped to sentenceFor's body on purpose.
    const sentenceFor = sw.slice(
      sw.indexOf("function sentenceFor"),
      sw.indexOf('self.addEventListener("push"'),
    );
    expect(sentenceFor).toContain('case "draft_activity"');
    expect(sentenceFor).toContain("draftVerb(p.draftAction)");
    // The null verb stops the sentence here: without this gate the payload
    // would render "… null …" and still raise a notification.
    expect(sentenceFor).toContain("if (verb === null) {");
    expect(sentenceFor).toContain(
      'p.actorUsername + " " + verb + " " + p.contentTitle',
    );

    // The deep link is the STAFF editor for BOTH content kinds — its content
    // is unpublished, so the public route would only mask it.
    const deepLink = sw.slice(
      sw.indexOf("function deepLinkPath"),
      sw.indexOf("function sentenceFor"),
    );
    expect(deepLink).toContain('if (p.kind === "draft_activity")');
    expect(deepLink).toContain('"/admin/projects/"');
    expect(deepLink).toContain('"/admin/blog/"');
    // …after the malformed-data guard, so a payload without a contentId is
    // dropped rather than linked to /admin/blog/undefined. Both indexes are
    // asserted first: without them a vanished guard would flip every
    // comparison below into a vacuous -1 < -1.
    const malformedGuard = deepLink.indexOf(
      "if (!p.contentKind || !p.contentId)",
    );
    const draftBranch = deepLink.indexOf('if (p.kind === "draft_activity")');
    expect(malformedGuard).toBeGreaterThan(-1);
    expect(draftBranch).toBeGreaterThan(-1);
    expect(malformedGuard).toBeLessThan(draftBranch);
  });

  test("uses the display contract (icon, per-content dedupe tag, deep-link data)", async () => {
    const sw = await readSw();
    expect(sw).toContain('icon: "/icons/icon-192.png"');
    // The tag falls back to contentId when the kind carries no commentId
    // (content_updated — repeats replace per content instead).
    expect(sw).toContain(
      '"sf-" + data.kind + "-" + (data.commentId || data.contentId)',
    );
    expect(sw).toContain("data: { path }");
    // The deep link maps blog-posts → /blog/… and projects → /projects/…
    expect(sw).toContain(
      'const segment = p.contentKind === "projects" ? "projects" : "blog"',
    );
    expect(sw).toContain('"#comment-" + p.commentId');
    // The comment-less kinds (content_updated, new_content) deep-link the
    // content page itself — the guards require contentKind + contentId,
    // not commentId.
    expect(sw).toContain("if (!p.contentKind || !p.contentId)");
    expect(sw).toContain('return "/" + segment + "/" + p.contentId;');
  });

  test("relays pushsubscriptionchange to open clients", async () => {
    const sw = await readSw();
    expect(sw).toContain(
      'postMessage({ type: "sf-push-subscription-change" })',
    );
  });
});
