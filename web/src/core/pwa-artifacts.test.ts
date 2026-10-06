/**
 * PWA artifact pins.
 *
 * These tests read the REAL public/ artifacts — not copies — and pin the
 * contracts the Go side depends on:
 *  - manifest.json parses, its description matches the catalog copy, and
 *    every declared icon exists as a PNG;
 *  - index.html carries the manifest/theme-color/iOS tags;
 *  - sw.js carries the SF_VERSION = "dev" placeholder that
 *    cmd/api/frontend.go replaces at serve time;
 *  - sw.js keeps the conservative update policy (no skipWaiting, no
 *    clients.claim).
 *
 * A web-build copy must never drift from public/: these files ARE the
 * source of truth the build copies into dist/.
 */

import { describe, expect, test } from "bun:test";

import { el } from "@shared/catalog/el.js";

function artifactPath(name: string): URL {
  return new URL(`../../public/${name}`, import.meta.url);
}

/** First 8 bytes of every PNG (the file signature). */
const PNG_MAGIC = new Uint8Array([
  0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
]);

async function readArtifact(name: string): Promise<string> {
  return Bun.file(artifactPath(name)).text();
}

describe("PWA manifest", () => {
  test("parses and declares the accepted fields", async () => {
    const manifest = JSON.parse(await readArtifact("manifest.json")) as Record<
      string,
      unknown
    >;
    expect(manifest.name).toBe("Sick-Fansubs");
    expect(manifest.short_name).toBe("Sick-Fansubs");
    expect(manifest.lang).toBe("el");
    expect(manifest.start_url).toBe("/");
    expect(manifest.scope).toBe("/");
    expect(manifest.id).toBe("/");
    expect(manifest.display).toBe("standalone");
    expect(manifest.theme_color).toBe("#0d0d12");
    expect(manifest.background_color).toBe("#0d0d12");
  });

  test("description matches the catalog copy (static files duplicate, the pin catches drift)", async () => {
    const manifest = JSON.parse(await readArtifact("manifest.json")) as {
      description: string;
    };
    expect(manifest.description).toBe(el.meta.description);
  });

  test("index.html links the manifest, theme colors, and iOS tags", async () => {
    const html = await Bun.file(
      new URL("../../index.html", import.meta.url),
    ).text();
    expect(html).toContain('<link rel="manifest" href="/manifest.json" />');
    expect(html).toContain('name="theme-color"');
    expect(html).toContain('content="#0d0d12"');
    expect(html).toContain('content="#f5f3f7"');
    expect(html).toContain(
      '<link rel="apple-touch-icon" href="/icons/apple-touch-icon.png" />',
    );
    expect(html).toContain('name="apple-mobile-web-app-capable" content="yes"');
    expect(html).toContain(el.meta.description);
    // The Go server replaces this exact marker at serve time (the footer's
    // app-version); an edit that drops it from the source must fail here,
    // because the serving tests carry their own MapFS fixture. The built dist
    // shape stays a deploy-smoke check.
    expect(html).toContain('name="app-version" content="dev"');
  });

  test("apple-touch-icon exists as a PNG (index.html links it, the manifest does not)", async () => {
    const bytes = new Uint8Array(
      await Bun.file(artifactPath("icons/apple-touch-icon.png")).arrayBuffer(),
    );
    expect(bytes.subarray(0, 8)).toEqual(PNG_MAGIC);
  });

  test("every declared icon exists as a PNG on disk", async () => {
    const manifest = JSON.parse(await readArtifact("manifest.json")) as {
      icons: { src: string; sizes: string; type: string; purpose: string }[];
    };
    expect(manifest.icons.length).toBeGreaterThanOrEqual(2);
    const sizes = manifest.icons.map((icon) => icon.sizes);
    expect(sizes).toContain("192x192");
    expect(sizes).toContain("512x512");
    for (const icon of manifest.icons) {
      expect(icon.purpose, icon.src).toBe("any");
      const bytes = new Uint8Array(
        await Bun.file(artifactPath(icon.src.replace(/^\//, ""))).arrayBuffer(),
      );
      expect(bytes.subarray(0, 8), icon.src).toEqual(PNG_MAGIC);
    }
  });
});

describe("PWA service worker", () => {
  test("carries the serve-time version placeholder", async () => {
    expect(await readArtifact("sw.js")).toContain(`SF_VERSION = "dev"`);
  });

  test("keeps the conservative update policy", async () => {
    const source = await readArtifact("sw.js");
    expect(source).not.toContain("self.skipWaiting");
    expect(source).not.toContain("self.clients.claim");
  });
});
