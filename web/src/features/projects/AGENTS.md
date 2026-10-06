# web/src/features/projects — Agent Navigation

Public projects feature: the projects list/detail endpoints + wire types (`data-access/`) and the list + detail views at the feature root.

## Open this file when

- changing the projects wire contracts, the list page, the detail page, or projects copy.

## Placement

- `data-access/types.ts` mirrors the Go projects handler DTOs and the OpenAPI operations `listProjects`/`getProject` — update both sides together (shared AGENTS.md convention).
- `data-access/projects-api.ts` owns `GET /api/v1/projects` (keyset-paginated, `pv1.` cursor namespace — cursors never cross endpoints) and `GET /api/v1/projects/{id}` (`getProject`, id URL-encoded as a path segment).
- `projects-list-page.ts` renders the four deliberate states (loading/empty/success/error) as a `<content-card shape="grid">` grid; `project-detail-page.ts` renders loading/error/notFound/success for `/projects/:id` and leads with `<content-card shape="detail">`. Both live at the feature root (route targets sit at `features/<feature>/<page>.ts`).

## Folder-local conventions

- **Detail page mirrors the blog structure, no subtitle, slug is data-only, ID-addressed URLs.** The detail page leads with `<content-card shape="detail">` (thumbnail → title row → stats → description → byline), the same shape the blog detail composes; the ONLY structural differences are the missing subtitle input (the projects table has none) and the null-guarded thumbnail. The list page has NO featured hero card (grid-only, `shape="grid"`). No subtitle element exists in any template — do not add one "for symmetry". `slug` is never an address key: public links are `/projects/{id}` exactly like `/blog/{id}`; the staff list shows it as the row's secondary data line and the admin+ edit form exposes the field. The staff edit affordance is a `slot="actions"` child of the card for moderator+ sessions — client-side visibility only; the server enforces — linking to `/admin/projects/{id}` (the blog detail's `.admin-edit-link` pattern) as an ICON-only pencil (`icons.edit`) whose `aria-label` + `title` carry `el.admin.edit`.
- Card titles are `h2` (the blog grid's `h3` sits under the hero's `h2`; here the sr-only `h1` is the only heading above the grid) — the page passes `heading-level="2"` to the card.
- The blog's download-layout exploration toggle is NOT ported (blog-only experiment).
- Each page owns ONE AbortController for its in-flight request — aborted on teardown and superseded by newer loads; a stale response must never paint over a newer one. The detail page also reloads when `projectId` changes (`willUpdate` + `hasUpdated` guard) — same rule as the blog pages.
- Pagination is the shared URL-carried cursor controller — `UrlCursorPagingController` (`shared/utils/url-cursor-paging.ts`) with the `shared/ui/pager-nav` UI; `blog/AGENTS.md` carries the shared contract (URL params, history marker, size-select, states, pager hiding).
- Dates render through the shared `formatDateTimeNumeric` (`shared/utils/format.js`) in the cards' meta lines — the numeric Greek date + time form every content list uses.
- Detail meta: `updatedAt` renders only when it is strictly after `publishedAt` (the card compares the parsed instants) — same policy as the blog detail page.
- Avatar chips (`shared/ui/avatar.ts` + `avatar.css`) are shared with the blog — identical shapes; `<content-card>` imports them for its grid and detail shapes. The ❝ ❞ quote treatment, the meta line, and the ❯❯ marker live inside the card element. `requestSignal` lives in `shared/api/request-signal.ts`.
- Download links (magnet/torrent) stay plain `<a>` elements — the app-shell interceptor passes non-http(s) schemes through natively (magnet: opens the torrent handler; never a pushState path). `lang="en"` on the labels (deliberate anglicisms, catalog comment).
- Thumbnails use `alt=""` — presentational; the adjacent title conveys the content.
- Detail descriptions: the ❝/❞ glyphs sit in the paragraph with collapsed whitespace; ONLY the `.description-text` span carries `white-space: pre-line` (data line breaks). Template line-layout must never move the closing glyph onto its own line — keep the pre-line scope on the text span, not the paragraph.
- The explicit page size of 10 mirrors the blog list's first-page preference.
- Follow bell: the detail page embeds `<follow-toggle kind="projects" content-id={project.id}>` after the `<favorite-toggle>` (the recorded cross-feature capability edge, components.md rule 2).
