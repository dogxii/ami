# Go migration plan

## Goal and constraints

Ship ami as a native single executable, keeping the lightweight task agent and all 11 tools. Preserve configuration, trust records, workspace-scoped JSONL sessions, model compatibility, permission enforcement, large-file/output pagination, cancellation, and Git workflows. No daemon, database, agent framework, embedded JavaScript engine, or language bridge. Use Go's standard library plus `golang.org/x/term` for portable terminal detection and hidden credential input.

## Implementation sequence

1. **Native entry and build:** `cmd/ami`, a small internal application package, build-time version, Go module, Makefile, native release matrix (macOS/Linux/Windows, amd64/arm64).
2. **Runtime and persistence:** existing config/env precedence, canonical workspace trust, run-scoped permissions, append-only JSONL v1 restoration (including TypeScript sessions), torn-record repair, uncertain tool results without replay, private artifact files and bounded UTF-8/binary pages.
3. **Tools and commands:** strict schema validation, safe paths, streaming file reads/search/listing, exact atomic edits and safe creation, Git inspection, Tavily, disk-backed subprocess output, stdin/poll/stop, timeout and process-group cleanup. Return incremental command pages to avoid repeated output.
4. **Provider and agent:** Chat Completions endpoint normalization, SSE/tool-call assembly, complete-response validation before actions, bounded retry and cancellation, usage-option fallback, read-only JSON compatibility, unlimited default loop, budgets, repeated-error stop, grouped history compaction, project instructions and piped input.
5. **User workflows and distribution:** config/init, sessions/output/trust, commit/push/rebase confirmations and conflict preservation, checksum-verified native updates, direct installer, optional dependency-free npm launcher. Switch development/release commands and remove the obsolete TypeScript implementation after parity checks pass.
6. **Acceptance:** local HTTP model fixtures, real subprocesses, large Unicode/binary files, stale edits and path escape checks, crash recovery, continuation, budget/compaction tests, CLI JSON and SIGINT integration, local bare Git remotes, update/installer fixtures, `go test -race`, `go vet`, platform builds and measured startup/binary size against the previous Node build.

## Compatibility decisions

- Keep command names, option names, environment variables, `~/.config/ami/config.json`, XDG/AMI_STATE_DIR state, v1 event names and Chat Completions message records.
- File versions are opaque; old recorded versions are not reusable after restarting either implementation. Re-read files on continuation.
- Existing trust is canonical-directory scoped; `--ask` and `--read-only` override it. Manual `ami tool` explicitly authorizes that operation.
- Shell commands still use host permissions. Preserve Unix process-group cleanup and document Windows orphan-tree limitations, no PTY or cross-invocation process reattachment.
- Native release assets are the primary distribution; npm remains an optional launcher, not the application runtime. Publishing releases/packages is separate from local implementation and validation.
- Keep installation/updates out of the startup path. Optional version notifications must not delay a completed task.

## Completion checklist

- [x] Native CLI and build
- [x] Config, permissions, sessions and artifacts
- [x] All 11 tools and lifecycle
- [x] Provider, task loop, compaction and stdin
- [x] Git workflows, installation and updates
- [x] Regression/race/vet/platform checks and benchmarks
- [x] Documentation, site and legacy removal

## Acceptance results

Implemented as ami 0.5.0. Acceptance requires native race/regression tests, go vet, npm/installer tests, all six native builds and CI runtime tests on Windows/Linux/macOS. Native release assets and checksums are published by the version-tag workflow; publish the npm launcher after those assets are available. Startup and local execution were measured on macOS ARM64.

Startup/local tool measurements and their limits are in [performance.md](performance.md). Core config, trust and JSONL v1 formats remain compatible; actual provider validation here uses local HTTP fixtures rather than paid production models. Windows orphan child cleanup, no PTY and no process reattachment remain documented limitations.
