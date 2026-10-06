/**
 * Download-rows widget tests —
 * the load/getValue contract, add/remove/reorder, and the row-label naming
 * per content type.
 */

import { beforeEach, describe, expect, test } from "bun:test";

import { el } from "@shared/catalog/el.js";
import { stripCssComments } from "@shared/api/test-utils.js";

import "./download-rows.js";
import type { DownloadRows } from "./download-rows.js";

// The stylesheet is read from disk rather than through `?inline`; the pins
// strip comments first (testing rule 7 — prose may name a retired value).
const rowsStyles = stripCssComments(
  await Bun.file(new URL("./download-rows.css", import.meta.url)).text(),
);

async function render(): Promise<DownloadRows> {
  const widget = document.createElement("download-rows") as DownloadRows;
  document.body.appendChild(widget);
  await widget.updateComplete;
  return widget;
}

function input(widget: DownloadRows, selector: string): HTMLInputElement {
  const el = widget.shadowRoot?.querySelector(
    selector,
  ) as HTMLInputElement | null;
  if (!el) throw new Error(`missing input ${selector}`);
  return el;
}

function click(widget: DownloadRows, selector: string) {
  const btn = widget.shadowRoot?.querySelector(
    selector,
  ) as HTMLButtonElement | null;
  if (!btn) throw new Error(`missing button ${selector}`);
  btn.click();
  return widget.updateComplete;
}

beforeEach(() => {
  document.body.innerHTML = "";
});

describe("download-rows", () => {
  test("starts with one empty row (the downloads array is required)", async () => {
    const widget = await render();
    expect(widget.shadowRoot?.querySelectorAll(".download-row").length).toBe(1);
  });

  test("a lone row carries no action controls, and cannot be emptied", async () => {
    // The array is required (≥ 1 row), so a lone row renders no actions at all:
    // nothing to reorder, and the remove control's only outcome would be an
    // empty array — which passes isValid() and reaches the server as a 422.
    // The guard below makes that state unreachable even if something calls the
    // private removal directly.
    const widget = await render();
    expect(widget.shadowRoot?.querySelectorAll(".row-button").length).toBe(0);
    expect(widget.shadowRoot?.querySelectorAll(".row-remove").length).toBe(0);
    // …and the fields take the freed tracks (the sheet's `data-solo` shape).
    const soloRow = widget.shadowRoot?.querySelector(".download-row");
    expect(soloRow?.getAttribute("data-solo")).toBe("true");

    widget.load([
      { label: "A", magnetUrl: "magnet:?xt=a", torrentUrl: null },
      { label: "B", magnetUrl: null, torrentUrl: "https://fans.example/b" },
    ]);
    await widget.updateComplete;
    expect(widget.shadowRoot?.querySelectorAll(".row-button").length).toBe(6);
    const rows = widget.shadowRoot?.querySelectorAll(".download-row");
    expect(rows?.[0]?.hasAttribute("data-solo")).toBe(false);

    // Back to one row: the actions retire with the row that was removed.
    const remove = widget.shadowRoot?.querySelectorAll(".row-remove");
    (remove?.[1] as HTMLButtonElement | undefined)?.click();
    await widget.updateComplete;
    expect(widget.getValue().map((r) => r.label)).toEqual(["A"]);
    expect(widget.shadowRoot?.querySelectorAll(".row-button").length).toBe(0);

    (widget as unknown as { _remove: (i: number) => void })._remove(0);
    await widget.updateComplete;
    expect(widget.getValue().length).toBe(1);
  });

  test("load round-trips stored rows and getValue trims + nulls empty links", async () => {
    const widget = await render();
    widget.load([
      { label: "1080p", magnetUrl: "magnet:?xt=one", torrentUrl: null },
      {
        label: " 2160p ",
        magnetUrl: null,
        torrentUrl: "https://fans.example/t.torrent",
      },
    ]);
    await widget.updateComplete;

    expect(widget.shadowRoot?.querySelectorAll(".download-row").length).toBe(2);
    expect(input(widget, ".download-label").value).toBe("1080p");
    expect(input(widget, ".download-link").value).toBe("magnet:?xt=one");

    const value = widget.getValue();
    expect(value).toEqual([
      { label: "1080p", magnetUrl: "magnet:?xt=one", torrentUrl: null },
      {
        label: "2160p",
        magnetUrl: null,
        torrentUrl: "https://fans.example/t.torrent",
      },
    ]);
  });

  test("add, remove, and reorder rows", async () => {
    const widget = await render();
    widget.load([
      { label: "A", magnetUrl: "magnet:?xt=a", torrentUrl: null },
      { label: "B", magnetUrl: "magnet:?xt=b", torrentUrl: null },
    ]);
    await widget.updateComplete;

    // Move the first row DOWN → B, A.
    await click(widget, ".download-row .row-button:nth-of-type(2)");
    expect(widget.getValue().map((r) => r.label)).toEqual(["B", "A"]);

    // Add a row → B, A, "".
    await click(widget, ".row-add");
    expect(widget.getValue().map((r) => r.label)).toEqual(["B", "A", ""]);

    // Remove the middle row → B, "".
    const removeButtons = widget.shadowRoot?.querySelectorAll(".row-remove");
    (removeButtons?.[1] as HTMLButtonElement | undefined)?.click();
    await widget.updateComplete;
    expect(widget.getValue().map((r) => r.label)).toEqual(["B", ""]);
  });

  test("reset returns to one empty row", async () => {
    const widget = await render();
    widget.load([
      { label: "A", magnetUrl: "magnet:?xt=a", torrentUrl: null },
      { label: "B", magnetUrl: "magnet:?xt=b", torrentUrl: null },
    ]);
    await widget.updateComplete;
    widget.reset();
    await widget.updateComplete;

    expect(widget.shadowRoot?.querySelectorAll(".download-row").length).toBe(1);
    expect(widget.getValue()).toEqual([
      { label: "", magnetUrl: null, torrentUrl: null },
    ]);
  });

  test("load of an empty stored set keeps one empty row (the required array)", async () => {
    const widget = await render();
    widget.load([]);
    await widget.updateComplete;

    expect(widget.shadowRoot?.querySelectorAll(".download-row").length).toBe(1);
    expect(widget.getValue()).toEqual([
      { label: "", magnetUrl: null, torrentUrl: null },
    ]);
  });

  test("isValid reports the one product rule the client pre-checks", async () => {
    const widget = await render();
    // The default empty row has no links.
    expect(widget.isValid()).toBe(false);

    widget.load([
      { label: "1080p", magnetUrl: null, torrentUrl: null },
      { label: "2160p", magnetUrl: "magnet:?xt=x", torrentUrl: null },
    ]);
    await widget.updateComplete;
    expect(widget.isValid()).toBe(false);

    widget.load([
      { label: "1080p", magnetUrl: "magnet:?xt=x", torrentUrl: null },
      {
        label: "2160p",
        magnetUrl: null,
        torrentUrl: "https://fans.example/t.torrent",
      },
    ]);
    await widget.updateComplete;
    expect(widget.isValid()).toBe(true);
  });

  test("renders the row-label copy from the property (resolution vs name)", async () => {
    const widget = await render();
    widget.rowLabel = el.admin.downloadResolutionLabel;
    await widget.updateComplete;
    expect(input(widget, ".download-label").getAttribute("aria-label")).toBe(
      el.admin.downloadResolutionLabel,
    );
  });

  test("the remove button shows the Greek copy", async () => {
    const widget = await render();
    widget.load([
      { label: "A", magnetUrl: "magnet:?xt=a", torrentUrl: null },
      { label: "B", magnetUrl: "magnet:?xt=b", torrentUrl: null },
    ]);
    await widget.updateComplete;
    expect(
      widget.shadowRoot?.querySelector(".row-remove")?.textContent?.trim(),
    ).toBe(el.admin.downloadRemoveRow);
  });

  test("the rendered row carries the area hooks the phone band places", async () => {
    // The band rules place these classes into named grid areas, so the markup
    // is part of the layout contract — a renamed class would silently un-style
    // the row. Two rows: a lone row renders no action controls.
    const widget = await render();
    widget.load([
      { label: "A", magnetUrl: "magnet:?xt=a", torrentUrl: null },
      { label: "B", magnetUrl: "magnet:?xt=b", torrentUrl: null },
    ]);
    await widget.updateComplete;
    const row = widget.shadowRoot?.querySelector(".download-row");

    expect(row?.querySelector(".download-label")).not.toBeNull();
    expect(row?.querySelector(".download-magnet")).not.toBeNull();
    expect(row?.querySelector(".download-torrent")).not.toBeNull();
    expect(row?.querySelector(".row-up")).not.toBeNull();
    expect(row?.querySelector(".row-down")).not.toBeNull();
    expect(row?.querySelector(".row-remove")).not.toBeNull();
    // The link inputs' accessible names are anglicisms — the scope rides the
    // input (the catalog's recorded intent).
    expect(input(widget, ".download-magnet").getAttribute("lang")).toBe("en");
    expect(input(widget, ".download-torrent").getAttribute("lang")).toBe("en");
  });

  test("the row shrinks and stacks instead of widening the page", async () => {
    // The owner's "content overflow": each link track kept a 10rem MINIMUM, so
    // the grid painted a ~670px row inside a ~400px viewport and the admin
    // form page scrolled sideways. A grid paints its track minimums whatever
    // its container is, so the minimums are the defect. happy-dom resolves no
    // CSS, so this is a source pin.
    const base = rowsStyles.match(/\.download-row \{([^}]*)\}/s)?.[1] ?? "";
    expect(base).toContain("minmax(0, 2fr)");
    expect(base).not.toMatch(/minmax\(10rem/);

    // Every child is placed by name, so the stacked shape does not depend on
    // the children's DOM order.
    for (const cls of [
      "download-label",
      "download-magnet",
      "download-torrent",
      "row-up",
      "row-down",
      "row-remove",
    ]) {
      expect(rowsStyles).toMatch(new RegExp(`\\.${cls} \\{[^}]*grid-area:`));
    }

    // The phone band: the fields take a full line each and the three actions
    // share their own right-aligned line under them (the band is the sheet's
    // last block, so slicing from the query reads it whole).
    const phone = rowsStyles.slice(
      rowsStyles.indexOf("@media (max-width: 700px)"),
    );
    expect(phone).toContain(
      "grid-template-columns: minmax(0, 1fr) auto auto auto",
    );
    expect(phone).toContain('"label label label label"');
    expect(phone).toContain('". up down remove"');
    // A lone row has no actions line: the same three fields, stacked.
    expect(phone).toContain('.download-row[data-solo="true"]');
    expect(phone).toContain('"torrent"');
    // The separator keeps the stacked rows apart now that their fields no
    // longer sit side by side.
    expect(phone).toContain("border-top: 1px solid");
    expect(phone).toContain(".download-row:first-child");

    // …and the lone-row shape outside the band: the fields take the tracks the
    // actions would have used, so their widths still match the form's fields.
    const solo =
      rowsStyles.match(
        /\.download-row\[data-solo="true"\] \{([^}]*)\}/s,
      )?.[1] ?? "";
    expect(solo).toContain("minmax(0, 2fr) minmax(0, 2fr)");
    expect(solo).toContain('grid-template-areas: "label magnet torrent"');
  });
});
