# internal/slug — Agent Navigation

Project slug generation and validation.

## Open this file when

- changing slug generation, the Greek→latin transliteration, the slug grammar, or the length bounds.

## Folder-local conventions

- This package owns the ONLY Greek→latin transliteration in the codebase. The search package's accent map folds Greek accents to GREEK letters (FTS5); this map transliterates to LATIN letters — the two maps are deliberately separate contracts. Do not try to share them.
- Generation (`generate`) transliterates: Greek runes (accented or not, upper or lower) map through `greekToLatin` (ELOT-743-style: θ→th, χ→ch, ψ→ps, φ→f, ξ→x, η→i, υ→y, β→v, γ→g); every other rune lowercases to `[a-z0-9]` or becomes a hyphen; hyphens collapse and the edges trim. Latin accented letters (é) are NOT specially folded — they degrade to hyphens under the strict slug grammar (the product's titles are Greek/English; an admin can hand-edit afterwards). The map covers the modern monotonic forms; polytonic (Greek Extended) forms are outside it and degrade to hyphens (recorded acceptance).
- An empty generation result means nothing survived transliteration — the create handler falls back to `p-<id>` so a create never fails on slug generation alone.
- `GenerateBase` truncates to `BaseMaxRunes` (90) so any collision suffix (through `-100`) still fits `MaxRunes` (100). `Valid` is the manual-edit grammar: `^[a-z0-9]+(-[a-z0-9]+)*$`, 1..100 runes.
- Uniqueness is NOT this package's job: the store's UNIQUE constraint on `projects.slug` is the race-free backstop; the create handler retries with `-2`, `-3`, … on `store.ErrSlugTaken`.
- Two tests pin the map: the alphabet test covers the 24 unaccented letters, and `TestGenerate_EveryMapEntry` pins every map entry (all 69, the accented/dialytika forms included) against a literal table with a count assertion — a deleted entry fails the count, a changed value fails the row.

## Authoritative docs

- [Identifier rules](../../docs/patterns/go/ids.md)
