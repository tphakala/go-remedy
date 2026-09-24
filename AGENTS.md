# AGENTS.md

Guidance for coding agents and contributors working in this repository. Read it before changing code.

## Project

`github.com/tphakala/go-remedy` is a Go client library for the BMC Remedy AR System REST API (`/api/arsys/v1` and `/api/jwt`). It is a library only: there is no `main` package, no binary, and no in-code version constant. Releases are `vX.Y.Z` git tags; pushing a tag opens a draft GitHub release.

Hard constraints:

- **Runtime is stdlib only.** `github.com/stretchr/testify` is the only dependency and it is used by tests alone. Do not add a runtime dependency without a strong reason agreed in the PR.
- **The public API is semver-bound.** `v1.x` tags exist. Exported identifiers in package `remedy` must not be removed or have their signatures changed. Adding a method to an exported interface (`EntryServicer`, `AttachmentServicer`, `RemedyClient`, `HTTPDoer`) breaks every consumer implementation and mock, so treat it as a breaking change.

## Layout

| Path | Contents |
| --- | --- |
| `client.go` | `Client`, `New`, token state and refresh, request construction (`newRequest`, `newJSONRequest`), response and error decoding, path helpers |
| `auth.go` | `Login`, `LoginWithAuth`, `Logout`, `IsAuthenticated`, bounded token read |
| `options.go` | `Option` (client) and `QueryOption` (queries) functional options, `buildQueryParams` |
| `entry.go` | `entryService`: Get, List, Create, Update, Delete, Merge, Location header fallback for empty 201 responses |
| `attachment.go` | `attachmentService`: streaming download and multipart upload through `io.Pipe` |
| `query.go` | `Query` builder for AR qualification strings, value formatting and field escaping |
| `errors.go` | Sentinel errors and `APIError` (with `Is` mapping HTTP status to sentinels) |
| `interfaces.go` | Exported service interfaces used for mocking |
| `types.go` | Wire types (`Entry`, `EntryList`, `Link`, `Field`), `SortOrder`, `DeleteOption` |
| `internal/queue` | Single-slot semaphore that serializes requests per client |
| `internal/ratelimit` | Token bucket limiter |
| `fuzz_test.go` | Fuzz targets for field-name escaping, Location header parsing and error-body parsing |
| `rules/` | ruleguard matchers (build tag `ruleguard`): generic modernization and hazard rules, plus go-remedy's own in `rules/remedy.go` |
| `rules/rulestest/`, `rules/testdata/probe/` | Test that runs golangci-lint over the probe package and checks every matcher fires where planted, and only there |

## Commands

`task check` is the full local gate (build, vet on amd64 and arm64, tidy and `go fix` check, lint, ruleguard probe, race tests). Run it before declaring any change done. The individual steps, which mirror CI (`.github/workflows/ci.yml`):

```bash
task tools        # install the pinned golangci-lint (.golangci-version) and govulncheck
task fmt          # gofumpt + goimports through golangci-lint
task lint         # golangci-lint config verify && golangci-lint run
task rules        # compile the matchers and run the probe test
task vet          # go vet on amd64 and arm64, plus the matchers
task tidy:check   # go mod tidy -diff, go mod verify, go fix -diff
task test:race    # go test -race -shuffle=on ./...
task fuzz         # each fuzz target for 20s
task vuln         # govulncheck ./... (also runs weekly in CI)
```

CI also cross-builds `go build ./...` for linux, darwin and windows on amd64 and arm64 and runs the tests on Linux (amd64 and arm64), macOS and Windows, so never add platform-specific code without build constraints for every target.

## Go version and modern Go

`go.mod` declares `go 1.27`, which is the minimum toolchain. The codebase targets Go 1.26 and later, and code must be written the way current Go is written, not the way it was written in 2018. The ruleguard matchers in `rules/` and `go fix -diff` both flag outdated idioms; treat their findings as errors. The `modernize` linter is deliberately off because the matchers carry the same patterns, and running both leaves the reported message to run order.

Required patterns (use these, do not reach for the older equivalent):

- `any`, never `interface{}`.
- Built-in `min`, `max` and `clear`.
- `for i := range n` for counted loops. Loop variables are per-iteration since Go 1.22, so never write `v := v` copies.
- `slices` and `maps` packages (`slices.Contains`, `slices.SortFunc`, `slices.Collect(maps.Keys(m))`, and so on) instead of hand-written loops or `sort.Slice`.
- Iterator APIs where they avoid allocation: `strings.SplitSeq`, `strings.FieldsSeq`, `strings.Lines`, and range-over-func (`iter.Seq`) for new sequence-producing helpers.
- `errors.Is` and `errors.As` (or the generic `errors.AsType[E]` from Go 1.26) for error inspection, never `==` or a type assertion on a possibly wrapped error.
- `new(expr)` (Go 1.26) when a pointer to a value is needed, instead of a temporary variable.
- `omitzero` rather than `omitempty` on struct-typed or slice JSON fields where the zero value should be omitted (already used in `types.go`).
- `sync.WaitGroup.Go` for spawning tracked goroutines, `sync.OnceFunc` / `sync.OnceValue` for lazy init.
- `http.NewRequestWithContext`, never `http.NewRequest`. Use `http.Method*` and `http.Status*` constants, never string or integer literals.
- In tests: `t.Context()` instead of `context.Background()`, `b.Loop()` in benchmarks, `testing/synctest` for code whose behavior depends on time or goroutine scheduling (the rate limiter and queue are candidates), `t.TempDir()` and `t.Setenv()` for filesystem and environment.

Do not rely on anything behind `GOEXPERIMENT`; the library must build with a stock toolchain. The `TODO(go1.26)` notes in `client.go` about `runtime/secret` are exploratory only.

## Architecture invariants

Break any of these and the library misbehaves against a real Remedy server even when tests pass.

1. **Every API call is serialized through the queue.** Remedy rejects concurrent requests from one user with Error 9093. Each service method calls `c.acquireAndRateLimit(ctx)` and then `defer c.queue.Release()` before building its request. `Release` panics if called without a matching successful `Acquire`, so release exactly once and only after `acquireAndRateLimit` returned nil. New endpoints must follow the same shape as the existing methods in `entry.go`.
2. **Token handling order.** `acquireAndRateLimit` calls `ensureValidToken` before acquiring the queue. Refresh uses double-checked locking on `refreshMu` and re-logs in through `loginInternal`, which does not touch the queue. `LoginWithAuth` uses `loginAcquireQueue` (queue plus rate limit, no token check) to avoid a circular dependency. Keep these paths separate.
3. **Every `newRequest` / `newJSONRequest` returns a `context.CancelFunc` that the caller owns.** Call it on every path. `doAndDecode` defers it; `attachmentService.Get` hands it to `attachmentReader.Close`. Response bodies are always closed.
4. **Bound what you read from the server.** Login reads at most `maxTokenSize` bytes. Any new endpoint that reads an unstructured body must use `io.LimitReader` in the same way.
5. **Escape everything that reaches a URL or qualification.** Path segments go through `url.PathEscape` (see `entryPath`, `entryIDPath`, `attachmentPath`), query parameters through `url.Values`, and field names in qualifications through `escapeFieldName`. `Query.Raw` is the one deliberate escape hatch and must stay documented as such.
6. **Credentials stay private.** The `credentials` struct has unexported fields so `%+v` and reflection-based loggers do not expose them. Never log, format or return usernames, passwords, auth strings or tokens, including in error messages.
7. **Shared state is lock-protected.** `token`/`tokenExpiry` are guarded by `tokenMu`, `credentials` by `credentialsMu`. Access them only through the existing accessors. All tests run with `-race`.

## Coding standards

- **Errors.** Wrap with `fmt.Errorf("doing thing: %w", err)`; the message describes the operation, lowercase, no trailing punctuation. Exported sentinel errors live in `errors.go` and their text starts with `remedy: `. Validate required arguments up front and return the matching sentinel (`ErrEmptyFormName`, `ErrEmptyEntryID`) before touching the queue. HTTP failures become `*APIError` through `parseAPIError`; extend `APIError.Is` if a new status needs a sentinel.
- **Context.** Every exported method that does I/O takes `ctx context.Context` as its first parameter. Never store a context in a struct (`containedctx` lint) and never substitute `context.Background()` for a caller's context.
- **Configuration** uses functional options (`Option` for `Client`, `QueryOption` for queries). New settings get a `WithXxx` option with a doc comment stating the default.
- **Doc comments** on every exported identifier, starting with the identifier name. The package overview lives in `client.go`.
- **Comments must be true.** Do not describe behavior of the Remedy server, the stdlib or another package unless it has been verified; cite the source or leave the claim out. When behavior changes, update every comment, doc comment and README section that describes it.
- **Complexity.** `gocognit` is capped at 12. Split functions (as `decodeCreatedEntry` was split out of `Create`) rather than suppressing the linter.
- **Lint suppressions.** `//nolint` must name the specific linter and give a reason after it (`//nolint:gosec // G304: path comes from the caller`); `nolintlint` rejects anything less. Prefer fixing the code.
- **Magic numbers.** `mnd` is enabled; name constants (see `maxTokenSize`, `defaultTimeout`).
- **Dependencies.** `depguard` allows only the standard library and this module in non-test code, and bans `log` there: a library returns errors, it does not log.
- **Formatting** is gofumpt plus goimports (`task fmt`), checked by `golangci-lint run`. Import groups: stdlib, then third party, then this module.
- **Prose style** in code, comments, docs and commit messages: no em or en dashes; use commas, colons, semicolons, parentheses or a new sentence.

## Testing

- Use `testify`: `require` for preconditions and anything whose failure makes the rest of the test meaningless, `assert` otherwise. `testifylint` enforces idiomatic usage (`require.NoError`, `assert.ErrorIs`, `assert.Len`, and so on).
- The HTTP layer is mocked through the `HTTPDoer` interface with `mockHTTPClient` in `client_test.go`; `setupAuthenticatedClient` in `entry_test.go` performs the login round trip first. Reuse these helpers instead of writing new mocks.
- Tests never contact a real server. Use `remedy.example.com` style hosts and dummy credentials; never commit real hostnames, form data or credentials.
- Test helpers call `t.Helper()` (`thelper` lint).
- Every new test must be able to fail: before committing, remove or break the production line it is meant to cover and confirm the test goes red. A test whose name promises a property it does not assert is a bug.
- Cover error paths (HTTP error bodies, malformed JSON, empty bodies, context cancellation) and not only the happy path. Concurrency-related code needs a test that runs under `-race` with real contention.
- Tests run with `-shuffle=on`, so they must not depend on execution order.
- Code that parses server responses or escapes user input gets a fuzz target in `fuzz_test.go` asserting invariants, not just the absence of panics. Add a new target to the `fuzz` matrix in `ci.yml` and to `task fuzz`. A crash found by fuzzing is committed under `testdata/fuzz/` as a regression seed.

## Lint rules (ruleguard)

- `rules/*.go` are ruleguard matchers loaded by gocritic (see `.golangci.yaml`). They carry the `ruleguard` build tag, so the normal toolchain ignores them. `failOn: all` makes a rule file that fails to load break the lint run instead of being dropped silently.
- The generic files come from a shared project template. go-remedy's own matchers live in `rules/remedy.go` and enforce the architecture invariants above: queue acquisition only through `acquireAndRateLimit` (outside `client.go` and `auth.go`), requests only through `c.do`, no unbounded `io.ReadAll` of a response body, no credential or token passed to `fmt` or `errors.New`, and no unescaped value appended to a URL path. They are gated to the root package and skip `_test.go` files.
- Every matcher needs a `// rule: Name` section in `rules/testdata/probe/` with a `// want "..."` annotation on each line that must fire, and unannotated lines for the cases that must not. `task rules` fails when a matcher has no probe, when a want is not matched, or when a finding has no want.
- Before relying on a new or edited matcher, break it and confirm `task rules` goes red. golangci-lint's cache is not keyed on the rule files, so run `golangci-lint cache clean` after editing one (`task rules` does).
- To silence a matcher at a call site, use `//nolint:gocritic // <reason>`.

## CI, dependencies and releases

- GitHub Actions are pinned to full commit SHAs with the version in a trailing comment (`uses: owner/action@<sha> # vN`). Keep that format when adding or bumping actions. Workflows use `permissions: contents: read` by default and `persist-credentials: false` on checkout.
- Dependabot opens weekly PRs for Go modules (`deps` prefix) and Actions (`ci` prefix).
- The golangci-lint version is pinned once in `.golangci-version`; CI and `task tools` both read it. Bump it there, then run `golangci-lint config verify` and `task check`.
- CodeQL and OpenSSF Scorecard run on `main`; do not weaken workflow permissions or unpin actions. Vulnerability reports go through private reporting (`SECURITY.md`).
- A pushed `vX.Y.Z` tag creates a draft release with generated notes. A tag containing a hyphen is marked as a pre-release.

## Commits and pull requests

- Conventional commit prefixes: `feat:`, `fix:`, `chore:`, `ci:`, `deps:`, `docs:`, `test:`, `refactor:`. Imperative mood, subject under about 72 characters.
- One logical change per PR. PRs are squash-merged into `main`.
- The PR description states what changed, why, and how it was verified (the commands above).
- Update `README.md` in the same PR when public API, defaults or documented behavior change.
- `docs/plans/` is gitignored for local planning notes; never force-add it.
