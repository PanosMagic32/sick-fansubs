# Reviews

## Purpose

The one agreed way to run a per-slice adversarial review before a commit or tag: the review lenses,
the shared context every reviewer receives, the read-only rule, and the main agent's gate ownership.

This is the diff review. The whole-application review that runs before a stable release is a
different procedure ([release-review.md](../ops/release-review.md)); it never re-litigates a slice
finding this review already closed.

## Rules

1. **State the fixed point and the complete scope.** A review names the commit or tag it starts
   from, the tracked working-tree changes, and every untracked file. `git diff` omits untracked
   files, so the scope comes from `git status --porcelain` plus the changed-path list. Reviewers
   inspect the changed files and the code they touch, not the whole tree.
2. **Spawn every lens in parallel.** Spec, Standards, Accessibility, Security, and any other lens
   the owner names start at the same time with read-only, disjoint review questions. Lenses run
   sequentially only when one lens's fixes would invalidate another lens's scope.
3. **Reviewers are read-only.** A reviewer reads, greps, and inspects git metadata. It never edits
   files, runs a build, test, lint, or format command, installs or updates a dependency, or starts a
   service. The main agent runs the gates before spawning and again after fixes.
4. **Every reviewer receives the same context pack.** The pack states the scope, fixed point,
   changed paths (untracked marked), governing pattern and feature docs, owner rulings, known
   constraints, the last gate result, and the output contract. Write it once under
   `.scratch/review/` or paste identical text into every prompt; a sub-agent cannot see the parent
   conversation.
5. **Skills are loaded on demand; `caveman` is always first.** Every agent loads the `caveman`
   skill before any other tool call. A reviewer then loads the skill that matches its lens from the
   globally installed skills. Loading a skill never authorizes edits, gates, network access, or
   dependency changes.
6. **Tool use is batched and read-only.** Prefer the repository tools (`read_file`, `grep`,
   `find_path`, `list_directory`) over shell commands. When a shell is needed, group related
   read-only commands into one call, use `--no-pager` for git, and never request network,
   filesystem-write, or unsandboxed approval. One discovery pass should need one approval at most.
7. **Findings are evidence, not code.** Every finding names its severity, lens, `file:line`, the
   observed evidence, the impact, and a recommended fix. Mark anything inferred rather than
   verified. A finding is a report to the main agent, never an edit.
8. **The main agent triages before fixing.** Verify each finding against the current code. Fix a
   blocker immediately. Take behavior, security, or policy choices to the owner and record the
   ruling in the pattern or feature doc that owns the fact. Never weaken a guard to make a finding
   disappear.
9. **A pre-existing issue is still reported.** "Outside this slice" is not a disposition. The owner
   decides fix now, park with a ruling, or accept; the review never silently drops the finding.
10. **Docs and tests travel with the fix.** A fix updates the owning pattern or feature doc, adds or
    adjusts the regression test, and leaves `make check` and `make docs-lint` green in the same
    commit.
11. **Re-review only what the fixes invalidated.** If a fix changes behavior another lens reviewed,
    run that lens again on the new diff. Otherwise the main agent validates the fixes with the gates
    and reports the result; a full re-review is not a substitute for testing.

## Pattern

### Lenses

| Lens          | Primary question                                                                                          |
| ------------- | --------------------------------------------------------------------------------------------------------- |
| Spec          | Does the change match the wire contract, feature docs, and accepted behavior?                             |
| Standards     | Does it follow the Go and web pattern docs, tests, comments, and architecture guards?                     |
| Accessibility | Are semantics, names, focus, keyboard use, target size, contrast, and new-tab cues correct?               |
| Security      | Are authentication, authorization, sessions, CSRF/origin, validation, secrets, and data exposure correct? |
| Platform      | Do the Makefile, CI workflow, Docker files, deploy docs, and config match the change?                     |

A review with four agents usually runs Spec, Standards (including Platform), Accessibility, and
Security. Add a lens when the owner names a concern those four do not cover.

### Context pack

```text
Scope: <branch> working tree against <fixed point>
Changed paths: <tracked list; mark untracked files>
Governing docs: <pattern docs, feature docs, wire contract>
Owner rulings: <accepted decisions and constraints>
Gate state: <command and result, run before spawn>
Out of scope: <earlier slices, dependency bumps outside the change>
Output: per-finding blocks plus a short "areas checked" list
```

### Sequence

1. The main agent runs the gates and records the result.
2. The main agent writes the context pack and lists every untracked file.
3. The main agent spawns all lenses in parallel, each with the same pack and its own question.
4. The reviewers report findings; none edits a file or runs a gate.
5. The main agent deduplicates and verifies findings, takes owner rulings, fixes, updates tests and
   docs, and reruns the gates.
6. The change is ready to commit or tag when no blocker or major finding is open and the final gate
   run is green.

### Skills

| Need                                    | Skill                |
| --------------------------------------- | -------------------- |
| Every session, every agent              | `caveman`            |
| Two-axis review (standards and spec)    | `code-review`        |
| Module boundaries and interfaces        | `codebase-design`    |
| Hard failure or performance finding     | `diagnosing-bugs`    |
| Security lens                           | `security-audit`     |
| Owner ruling or stress test             | `grilling`           |
| Terminology or a recorded ruling        | `domain-modeling`    |
| Regression test or a fix                | `tdd`                |
| Editing this procedure or a feature doc | `writing-for-agents` |
| Primary-source research                 | `research`           |
| Discovering another installed skill     | `find-skills`        |

## Examples

A lens prompt carries the pack, the lens question, and the read-only line:

```text
Load the caveman skill first. Read .scratch/review/context.md.

You are the Accessibility reviewer. Review only the changed files against the accessibility
lens. Report findings with severity, file:line, evidence, impact, and a recommended fix.
Do not edit files. Do not run make, bun, go, tests, lint, or format commands. Do not install or
update dependencies. Use read-only tools; group shell commands into one call.
```

A recorded finding:

```text
A11Y-03 (major) web/src/core/shell/app-footer.ts:42
Evidence: the icon links open a new tab but carry no accessible new-tab cue.
Impact: screen-reader users lose the context change when the tab opens.
Fix: add the shared hidden new-tab suffix from the Greek catalog.
Confidence: confirmed
```

## Gotchas

- **`git diff` hides new files.** Use `git status --porcelain` as the scope source and pass the
  untracked paths to every reviewer.
- **A sub-agent starts empty.** It cannot see the parent conversation, the handoff, or an earlier
  reviewer's findings; the context pack must stand alone.
- **A reviewer that runs the gates wastes the batch.** The main agent owns the gate before spawn and
  after fixes; a reviewer reports what it could not verify.
- **Overlapping lenses duplicate findings.** Give each lens one primary question, then deduplicate
  by `file:line` when the reports arrive.
- **"Pre-existing" is not a ruling.** Report it and let the owner decide; an accepted issue needs a
  recorded disposition.
- **A green pre-fix gate is not a green post-fix gate.** Rerun the full gate after the last edit.
- **Dependency bumps have their own review surface.** Check the manifest and lockfile, CI pins,
  Docker tags, and any version-sensitive code or docs; never run an update command during review.

## Pointers

- Whole-app release review: [../ops/release-review.md](../ops/release-review.md)
- Documentation rules and skeletons: [docs-conventions.md](docs-conventions.md)
- Root working rules: [../../AGENTS.md](../../AGENTS.md)
- Wire contract: [../api/openapi.yaml](../api/openapi.yaml)
