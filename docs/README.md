# Documentation Index

This is the documentation entry point. Everything else is either co-located with the code it
describes or linked from this page.

## Doc kinds

| Kind          | Lives                                                                              | Answers                                                           |
| ------------- | ---------------------------------------------------------------------------------- | ----------------------------------------------------------------- |
| Pattern       | `docs/patterns/**`                                                                 | What is the one agreed way to do this?                            |
| Feature doc   | `AGENTS.md` beside the code (`internal/<pkg>/`, `cmd/<tool>/`, `web/src/<scope>/`) | What does this scope do, what are its contracts, its gotchas?     |
| Operations    | `docs/ops/**`                                                                      | How do we deploy, back up, restore, cut over?                     |
| Architecture  | [`docs/architecture.md`](architecture.md)                                          | What is the system's shape: context, containers, flows, topology? |
| Wire contract | `docs/api/openapi.yaml`                                                            | What are the endpoints, statuses, and payloads?                   |
| Index         | this file                                                                          | Where does everything live?                                       |

The wire contract is [`api/openapi.yaml`](api/openapi.yaml). Production-data evidence lives in
`docs/current-data/`, which is local-only and never published.

How to write and structure any of these: [patterns/docs-conventions.md](patterns/docs-conventions.md).

## Patterns

| Doc                                                          | Covers                                                                    |
| ------------------------------------------------------------ | ------------------------------------------------------------------------- |
| [patterns/docs-conventions.md](patterns/docs-conventions.md) | Documentation structure, skeletons, links, the docs guard                 |
| [patterns/go/structure.md](patterns/go/structure.md)         | Package layout, naming, imports, interfaces, statements                   |
| [patterns/go/comments.md](patterns/go/comments.md)           | Doc comments, package comments, in-body comments                          |
| [patterns/go/testing.md](patterns/go/testing.md)             | Test names, tables, failure messages, fixtures                            |
| [patterns/go/logging.md](patterns/go/logging.md)             | Request-scoped loggers, record shape, levels, hygiene                     |
| [patterns/go/errors.md](patterns/go/errors.md)               | Error vocabularies, wrapping, boundary mapping, codes                     |
| [patterns/go/route-chains.md](patterns/go/route-chains.md)   | Middleware chains, their order, where they are composed                   |
| [patterns/go/collections.md](patterns/go/collections.md)     | Keyset collections, the shared envelope, items-only tails                 |
| [patterns/go/generics.md](patterns/go/generics.md)           | Type parameters and the generic standard-library helpers                  |
| [patterns/go/ids.md](patterns/go/ids.md)                     | Identifier minting, storage shapes, the store/media edge                  |
| [patterns/go/sql-mapping.md](patterns/go/sql-mapping.md)     | NULL mapping, time representation, scan rules                             |
| [patterns/go/sqlite.md](patterns/go/sqlite.md)               | Driver, DSN/pragmas, WAL, pool, transactions, migrations, backup/restore  |
| [patterns/go/content-kinds.md](patterns/go/content-kinds.md) | Content-kind descriptors, shared readers, the per-kind write transactions |
| [patterns/go/toolchain.md](patterns/go/toolchain.md)         | The pinned toolchain, its answers, the refresh procedure                  |
| [patterns/web/components.md](patterns/web/components.md)     | Feature layout, composition, controls, forms, dialogs, accessibility      |
| [patterns/web/css.md](patterns/web/css.md)                   | Sheets and tokens, colour rules, breakpoints, focus, layout guards        |
| [patterns/web/testing.md](patterns/web/testing.md)           | The Bun/happy-dom test setup, pins, and the architecture guards           |
| [patterns/web/toolchain.md](patterns/web/toolchain.md)       | The pinned web toolchain, recorded divergences, the refresh procedure     |

## Operations

How the system runs against the real VPS: deployment and rollback, the backup lifecycle, the
staged restore and its drill, and the production cutover.

| Doc                                            | Covers                                                                             |
| ---------------------------------------------- | ---------------------------------------------------------------------------------- |
| [ops/deploy.md](ops/deploy.md)                 | Topology, the tag-driven pipeline, the deploy/rollback sequence, secrets           |
| [ops/backup.md](ops/backup.md)                 | The daily unit, encryption, retention, triggers, the off-VPS push, bring-up        |
| [ops/restore.md](ops/restore.md)               | The fail-closed staged restore and the quarterly loss-of-VPS drill                 |
| [ops/cutover.md](ops/cutover.md)               | The production cutover, the rollback window, and closing out the legacy stack      |
| [ops/currency.md](ops/currency.md)             | Dependency currency: Dependabot, the weekly drift/vulnerability report, VPS checks |
| [ops/release-review.md](ops/release-review.md) | Whole-app review before stable cutovers: lenses, passes, findings, gates           |

## Feature docs

The package and scope map lives in the root `AGENTS.md`; each entry points at the co-located
`AGENTS.md` for that scope.
