# web/src/features/about — Agent Navigation

Static "Η ομάδα" page at `/about`. No data fetching, no states, no API — copy in the catalog, URLs in `shared/config/links.ts`.

## Open this file when

- changing the about page copy, its structure, or the community links.

## Folder-local conventions

- Copy lives in `el.about.*` (the six-member roster, the «Donate» label, the intro typo fix). Link labels are anglicisms rendered inside `lang="en"`; the lowercase tracker loanword renders plain (the shared catalog's anglicism rule). The donation link's `--sf-brand-coffee`/`.tone-coffee` names stay: they describe the URL target, not the label.
- The member line shows PUBLIC NAMES ONLY — roles are account state and must never appear on this page.
- App version + health are DELIBERATELY not on this page — the app-footer owns both (single source, no duplication).
- URLs come from `shared/config/links.ts`; per-environment link config is deferred future work.
- External anchors carry `target="_blank" rel="noopener noreferrer"` — the app-shell interceptor passes any `target` link through natively.
- The link text uses `--sf-link-on-tint` and a default underline: the light-theme accent drops below 4.5:1 on the brand tints, and color alone must never be the only cue that a sentence contains a link.

## Authoritative docs

- The copy group: `web/src/shared/catalog/el.ts` (`el.about`, plus `el.nav.about`/`el.titles.about`).
- The community URLs: `web/src/shared/config/links.ts`.
