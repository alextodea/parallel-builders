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

## Architecture

Every box below is a Go package. Colour is status, not category.

```mermaid
flowchart TD
    CLI["<b>cmd/pb</b><br/>init · run · bench · doctor"]:::partial
    CFG["<b>internal/config</b><br/>who the agents are,<br/>what the gate runs"]:::partial

    CLI --> CFG
    CLI --> ORCH

    ORCH["<b>cmd/pb · cmdRun</b><br/>the five phases in order<br/>— NOT WRITTEN YET —"]:::missing

    ORCH -->|1| SPEC
    ORCH -->|2| RACE
    ORCH -->|3| GATE
    ORCH -->|4| SEL
    ORCH -->|5| REP

    SPEC["<b>spec</b><br/>architect writes the tests,<br/>they get frozen"]:::phase
    RACE["<b>race</b><br/>builders run in parallel,<br/>one worktree each"]:::phase
    GATE["<b>gate</b><br/>build · lint · tests"]:::phase
    SEL["<b>select</b><br/>who won, or what next"]:::phase
    REP["<b>report</b><br/>one JSON line per run"]:::phase

    SPEC --> AGENT
    SPEC --> FROZEN
    RACE --> POOL
    RACE --> AGENT
    GATE --> GATEPKG
    GATE --> FROZEN
    SEL --> SELECTOR
    SEL --> ESCALATE
    REP --> RECORD

    AGENT["<b>internal/agent</b><br/>Runner interface<br/>Exec · Fake"]:::done
    FROZEN["<b>internal/frozen</b><br/>builders may not<br/>edit the tests"]:::done
    SELECTOR["<b>internal/selector</b><br/>disqualify, then<br/>the tie-break ladder"]:::done
    ESCALATE["<b>internal/escalate</b><br/>same failures → architect<br/>different → self-repair"]:::done
    POOL["<b>internal/pool</b><br/>reusable worktrees"]:::stub
    GATEPKG["<b>internal/gate</b><br/>run commands,<br/>report what failed"]:::partial
    RECORD["<b>internal/record</b><br/>JSONL"]:::partial
    BENCH["<b>internal/bench</b><br/>race-2 vs solo-cheap<br/>vs solo-frontier"]:::stub

    CLI -.-> BENCH

    classDef done fill:#e2f0e8,stroke:#1c6640,stroke-width:2px,color:#14201e
    classDef partial fill:#fbeadf,stroke:#b04a18,stroke-width:2px,color:#1a1a1d
    classDef stub fill:#eeeeee,stroke:#999999,stroke-width:1px,stroke-dasharray:4 3,color:#555555
    classDef missing fill:#ffffff,stroke:#b04a18,stroke-width:2px,stroke-dasharray:6 4,color:#b04a18
    classDef phase fill:#f7f7f8,stroke:#bcbcc4,stroke-width:1px,color:#1a1a1d
```

🟩 implemented and tested  🟧 partial  ⬜ stub  ⬛ not written

### The honest read of that picture

**The leaves are real. The trunk is not.**

Every `internal/` package currently imports *nothing* from this repo — they are independent, pure, and unit-testable, which is why three of them are already finished and covered. But it also means **nothing calls anything yet**. `cmdRun` is where they get assembled, and it is a list of TODOs.

That ordering was deliberate. The subtle parts are the leaves — what disqualifies a candidate, how you tell a spec problem from a coding mistake, which glob counts as a test file. Those are pure functions, so they can be got right in isolation and proven with tests. The wiring is mechanical by comparison, but it can't be written until `pool` and `gate` are real, because it has nothing to hold.

**Read it in this order** if you want to understand the tool: `selector` → `escalate` → `frozen`. Roughly 400 lines, no dependencies, and between them they contain every decision the system makes.

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

## Licence

MIT
