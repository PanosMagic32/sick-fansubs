-- 0005_search_index.sql — FTS5 search index (decision 0018, amending 0011).
--
-- The migration runner owns the transaction; do not add BEGIN/COMMIT/ROLLBACK
-- statements here.
--
-- Internal-content FTS5 tables (0018 §Storage): the single `text` column
-- holds Go-normalized text (Greek accents stripped) because the unicode61
-- tokenizer cannot strip precomposed Greek diacritics — normalization
-- happens in Go on BOTH the indexed text and every query token (0018
-- §Normalization). remove_diacritics 2 is kept for its Latin-diacritic
-- folding.
--
-- Synchronization is Go-owned (import tool, rebuild command, future CRUD
-- stores), NOT SQL triggers: 0011 §6's trigger sketch is invalid SQL and
-- its external-content 'delete' command corrupts on non-indexed rows
-- (0018 §Empirical findings). Rows without a publish time are never
-- indexed; queries also filter status='published' AND
-- published_at_ms IS NOT NULL.
--
-- Backfill: migration files cannot normalize Greek (pure SQL), so the
-- backfill for pre-existing content runs as a Go command:
-- `make rebuild-search-local` (cmd/rebuildsearch).

CREATE VIRTUAL TABLE blog_post_search USING fts5(
    text,
    tokenize='unicode61 remove_diacritics 2'
);

CREATE VIRTUAL TABLE project_search USING fts5(
    text,
    tokenize='unicode61 remove_diacritics 2'
);
