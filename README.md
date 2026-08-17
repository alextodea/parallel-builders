# parallel-builders

**One expensive agent writes the tests. Two cheap agents race to satisfy them. A compiler picks the winner.**

The usual way to use a coding agent is to ask an expensive model for a change and then read the diff. That puts you — the slowest component — in the loop on every change.

`pb` moves the expensive model to where it earns its cost, and takes you out of the middle.

```
pb run "add rate limiting to the public API routes"

  spec     architect wrote 7 tests · frozen at a1b2c3d     $0.014
  race     builder-a  worktree 1                           $0.048
           builder-b  worktree 2                           $0.052
  gate     builder-a  build ✓  lint ✓  tests 5/7   FAIL
           builder-b  build ✓  lint ✓  tests 7/7   PASS
  select   builder-b — only passing candidate
  ship     feat/rate-limit · 2 files, +34 −6
```

## How it works

**One expensive call, at the front.** The architect reads the task and writes a test suite — and nothing else. That suite is committed and **frozen**: it is the definition of "correct" for this change, and it is read-only from that point on.

**Cheap models race.** Two builders get the same task, the same frozen tests, and their own git worktree. Neither sees the other's work. Using two different model families matters — the value of racing comes from *independent* errors.

**A compiler decides.** Build, lint, and the frozen tests. No model is consulted, so selection is deterministic, free, and repeatable. When more than one candidate passes, a ladder of computable tie-breakers picks the winner: smallest diff, then fewest files, then fewest new dependencies.

**Nobody reads the losing diff.** Including you.

## Two rules that make it work

**Builders may not edit the tests.** Enforced as a check on the diff, not as an instruction in a prompt. A builder told "make the tests pass" will, given the chance, weaken an assertion — and a green run looks identical either way.

**Tests must fail before the change exists.** New tests are run against the previous commit; any that *pass* are testing nothing and get rejected. This is free, needs no model, and catches a bad specification before you have paid two builders to satisfy it.

## When they all fail

Comparing the two sets of failing test names costs nothing and decides what happens next:

| | |
|---|---|
| **Failed the same test, the same way** | Two independent attempts got stuck in the same place — the *specification* is the likely problem. Escalate. |
| **Failed different tests** | Two ordinary mistakes. Each builder gets its own diff and its own failures and returns a patch. No expensive call. |

Repair is not regeneration. "Try again" costs what the first round cost; handing back the diff and the specific failure produces a small patch instead. Same model, same round, a fraction of the bill.

Capped at two rounds, then it comes to you.

## Status

Early. The design is settled; most of the implementation is not.

| | |
|---|---|
| `internal/frozen` | ✅ the test-file guard |
| `internal/selector` | ✅ disqualifiers and the tie-break ladder |
| `internal/escalate` | ✅ the failure-set comparison |
| `internal/agent` | ✅ runner interface, exec and fake |
| `internal/gate`, `record`, `config` | 🚧 partial |
| `internal/pool`, `bench` | ⛔ stubs |

## Building

```sh
go build ./cmd/pb
go test ./...
```

No dependencies yet, on purpose.

## Design notes

Written up in more detail before any code existed:

- **[Two Builders, One Gate](https://claude.ai/code/artifact/a66749fa-4c1b-452d-af7c-c714ed9bbc34)** — the build spec: phases, contracts, worked runs, benchmark design
- **[Tests Before Judges](https://claude.ai/code/artifact/77370d62-bf8f-4520-9862-28e7a93da50b)** — why the architecture is shaped this way

## Prior art

[`no-mistakes`](https://github.com/kunchenguid/no-mistakes) and [`treehouse`](https://github.com/kunchenguid/treehouse) by Kun Chen solve overlapping problems and are worth reading. Two ideas here are borrowed directly: the auto-fix / ask-user split in how findings are handled, and re-reviewing a fix in a *fresh* session so the reviewer certifying a change is never the one that demanded it.

## Licence

MIT
