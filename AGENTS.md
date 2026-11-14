# MarkStack Development Instructions

## Scope And Source Of Truth

- Build a local-first Go + Viva native application. Do not embed a WebView, add cloud persistence or silently convert document types.
- When available locally, read `docs/specs/001-markstack/v0.2-design.md` and the capability's existing plan before implementation. Reuse local work packages; never add them to Git. A public checkout uses the confirmed Issue scope and acceptance conditions instead of requiring private files.
- Follow `CONTRIBUTING.md`: issue, confirmed scope/acceptance, `/claim` and bot-confirmed assignment, local Spec/plan, implementation, source-only PR, CI and evidence-based acceptance. Keep docs, plans, screenshots and execution ledgers local, including in PR/Issue attachments. Do not disguise them under other paths.
- Treat the initial repository import as a one-time bootstrap exception to issue ownership. Subsequent work requires an owned issue. Do not claim all of Markdown A or v0.2 complete from a passing slice.

## Engineering

- Use an isolated worktree and preserve unrelated changes. Do not commit, push, merge, release or clean existing work without the user's corresponding authorization.
- Keep Markdown/HTML raw source authoritative. Patch verified source intervals; never reserialize an entire AST to save a local edit.
- Preserve untouched bytes, UTF-8 BOM, CRLF/LF, permissions, unknown nodes, dirty drafts and external-change guards. Non-UTF-8 and ambiguous structures must be rejected or explicitly read-only/source fallback.
- Keep all input methods, modes and document kinds on shared save/recovery/history contracts. Uncommitted composition text must not enter committed source or disk.
- Verify a reusable Viva defect before changing the framework. Use its separate worktree and authorization; do not replace the pinned dependency with a developer's absolute path.

## Validation

- Add regression coverage for changed behavior. Start with focused tests, then `go test ./... -count=1`, `go test -tags vivahead ./... -count=1`, vet and formatting.
- Run `node --test .github/scripts/*.test.cjs` for automation changes. Keep comment/PR content as API data, never shell or JavaScript source.
- Use isolated test workspaces and explicit private `MARKSTACK_*_PATH` state files. Never exercise destructive scenarios on user documents or recovery journals.
- Keep automated tests, native input, reference screenshots, unsigned packages and formal releases as separate acceptance states. Current-package evidence must identify the source and binary it tested.
- Close only task-owned browser/test sessions. Do not clear shared Go caches, remove worktrees or discard other applications' drafts as incidental cleanup.
