---
name: diagnose
description: Evidence-driven root-cause diagnosis for bugs, errors, failures, flaky behavior, and performance regressions. Build a tight red-capable reproduction, minimise it, test ranked falsifiable hypotheses in a loop, prove the causal root cause, and prepare a regression-ready handoff. Use when the user asks to diagnose or debug something, reports broken or failing behavior, shares an error, or describes a performance regression.
---

# Diagnose

A discipline for hard bugs. Skip phases only when explicitly justified.

When exploring the codebase, use the project's domain glossary to get a clear mental model of the relevant modules, and check ADRs in the area you're touching.

When semantic language tooling is available, use it first for symbols, definitions,
implementations, references, types, and call hierarchy. Use `rg` for exact literals,
generated files, unsupported artifacts, or when semantic tooling cannot answer.
Verify important results against source before treating them as diagnostic evidence.

## Protect sensitive evidence

Redact credentials, tokens, cookies, authorization headers, private keys, and
sensitive payload fields from commands, output, logs, traces, and captured
artifacts before showing them. Keep credentials in environment variables rather
than reproduction scripts. If redacted evidence is insufficient, explain what is
missing and ask the user for a safer diagnostic path.

## Phase 1 — Build a feedback loop

**This is the skill.** Everything else is mechanical. If you have a fast, deterministic, agent-runnable pass/fail signal for the bug, you will find the cause — bisection, hypothesis-testing, and instrumentation all just consume that signal. If you don't have one, no amount of staring at code will save you.

Spend disproportionate effort here. **Be aggressive. Be creative. Refuse to give up.**

### Ways to construct one — try them in roughly this order

1. **Failing test** at whatever seam reaches the bug — unit, integration, e2e.
2. **Curl / HTTP script** against a running dev server.
3. **CLI invocation** with a fixture input, diffing stdout against a known-good snapshot.
4. **Headless browser script** (Playwright / Puppeteer) — drives the UI, asserts on DOM/console/network.
5. **Replay a captured trace.** Save a real network request / payload / event log to disk; replay it through the code path in isolation.
6. **Throwaway harness.** Spin up a minimal subset of the system (one service, mocked deps) that exercises the bug code path with a single function call.
7. **Property / fuzz loop.** If the bug is "sometimes wrong output", run 1000 random inputs and look for the failure mode.
8. **Bisection harness.** If the bug appeared between two known states (commit, dataset, version), automate "boot at state X, check, repeat" so you can `git bisect run` it.
9. **Differential loop.** Run the same input through old-version vs new-version (or two configs) and diff outputs.
10. **HITL bash script.** Last resort. If a human must click, drive _them_ with `scripts/hitl-loop.template.sh` so the loop is still structured. Captured output feeds back to you.

Build the right feedback loop, and the bug is 90% fixed.

### Tighten the loop

Treat the loop as a product. Once you have _a_ loop, ask:

- Can I make it faster? (Cache setup, skip unrelated init, narrow the test scope.)
- Can I make the signal sharper? (Assert on the specific symptom, not "didn't crash".)
- Can I make it more deterministic? (Pin time, seed RNG, isolate filesystem, freeze network.)

A 30-second flaky loop is barely better than no loop. A 2-second deterministic loop is a debugging superpower.

### Non-deterministic bugs

The goal is not a clean repro but a **higher reproduction rate**. Loop the trigger 100×, parallelise, add stress, narrow timing windows, inject sleeps. A 50%-flake bug is debuggable; 1% is not — keep raising the rate until it's debuggable.

### When you genuinely cannot build a loop

Stop and say so explicitly. List what you tried. Ask the user for: (a) access to whatever environment reproduces it, (b) a captured artifact (HAR file, log dump, core dump, screen recording with timestamps), or (c) permission to add temporary production instrumentation. Do **not** proceed to hypothesise without a loop.

Do not proceed to Phase 2 until you have a loop you believe in.

### Completion criterion: a tight loop that goes red

Phase 1 is complete only when you can name one command that you have already run
and whose redacted output demonstrates that it is:

- **Red-capable** — it reaches the actual bug path and asserts the user's exact
  symptom, rather than merely checking that execution completes.
- **Deterministic** — it produces a stable verdict, or a measured and sufficiently
  high reproduction rate for a flaky bug.
- **Fast** — narrow enough to run repeatedly while testing hypotheses.
- **Agent-runnable** — unattended unless the provided HITL template is genuinely
  necessary.

If you start forming a code-level theory before this command exists, stop and
finish the loop first.

## Phase 2 — Reproduce and minimise

Run the loop. Watch the bug appear.

Confirm:

- [ ] The loop produces the failure mode the **user** described — not a different failure that happens to be nearby. Wrong bug = wrong fix.
- [ ] The failure is reproducible across multiple runs (or, for non-deterministic bugs, reproducible at a high enough rate to debug against).
- [ ] You have captured the exact symptom (error message, wrong output, slow timing) so later phases can verify the fix actually addresses it.

Do not proceed until you reproduce the bug.

Shrink the reproduction to the smallest scenario that remains red. Remove one
input, caller, configuration value, data dependency, or action at a time and rerun
the loop after every removal. Keep only elements proven to be load-bearing. The
minimal reproduction should narrow the hypothesis space and, when possible,
become the regression test.

Do not proceed until removing any remaining relevant element makes the loop go
green or changes the reported symptom.

## Phase 3 — Hypothesise

Generate **3–5 ranked hypotheses** before testing any of them. Single-hypothesis generation anchors on the first plausible idea.

Each hypothesis must be **falsifiable**: state the prediction it makes.

> Format: "If <X> is the cause, then <changing Y> will make the bug disappear / <changing Z> will make it worse."

If you cannot state the prediction, the hypothesis is a vibe — discard or sharpen it.

**Show the ranked list to the user before testing.** They often have domain knowledge that re-ranks instantly ("we just deployed a change to #3"), or know hypotheses they've already ruled out. Cheap checkpoint, big time saver. Don't block on it — proceed with your ranking if the user is AFK.

## Phase 4 — Test hypotheses in a root-cause loop

Test the highest-ranked hypothesis with the smallest probe that distinguishes it
from the alternatives. Each probe must map to a prediction from Phase 3. **Change
one variable at a time.**

Tool preference:

1. **Debugger / REPL inspection** if the env supports it. One breakpoint beats ten logs.
2. **Semantic language navigation**: trace definitions, implementations, references,
   types, and callers before inferring a path from text matches.
3. **Targeted logs** at the boundaries that distinguish hypotheses.
4. Never "log everything and grep".

**Tag every debug log** with a unique prefix, e.g. `[DEBUG-a4f2]`. Cleanup at the end becomes a single grep. Untagged logs survive; tagged logs die.

**Perf branch.** For performance regressions, logs are usually wrong. Instead: establish a baseline measurement (timing harness, `performance.now()`, profiler, query plan), then bisect. Measure first, fix second.

After each probe:

1. Rerun the tight loop and record the redacted observation.
2. Mark the hypothesis supported, weakened, or falsified; do not silently rewrite
   it to fit new evidence.
3. Remove temporary instrumentation that no longer serves the next probe.
4. Test the next ranked hypothesis.

If every hypothesis is falsified, return to Phase 2 and minimise further when the
reproduction still contains uncertainty; otherwise return to Phase 3 and generate
a new ranked set from the evidence. Repeat this loop until one causal explanation
survives deliberate attempts to falsify it.

### Root-cause criterion

Do not call the nearest failing line or downstream error the root cause. A root
cause is established only when:

- the evidence explains the causal chain from triggering condition to exact symptom;
- a targeted intervention at the proposed cause changes the loop from red to green;
- reverting that intervention, or an equivalent counterfactual probe, makes it red
  again when practical; and
- competing hypotheses have been falsified or explicitly bounded as residual risk.

Do not begin the production fix until this criterion is met.

When an orchestration workflow owns implementation, return the reproduction,
causal chain, proof, ruled-out hypotheses, and regression seam to that workflow and
stop before production edits. When invoked for diagnosis only, report the same
handoff and wait for explicit implementation authority.

## Phase 5 — Fix + regression test

Write the regression test **before the fix** — but only if there is a **correct seam** for it.

A correct seam is one where the test exercises the **real bug pattern** as it occurs at the call site. If the only available seam is too shallow (single-caller test when the bug needs multiple callers, unit test that can't replicate the chain that triggered the bug), a regression test there gives false confidence.

**If no correct seam exists, that itself is the finding.** Note it. The codebase architecture is preventing the bug from being locked down. Flag this for the next phase.

If a correct seam exists:

1. Turn the minimised repro into a failing test at that seam.
2. Watch it fail.
3. Apply the fix.
4. Watch it pass.
5. Re-run the Phase 1 feedback loop against the original (un-minimised) scenario.

## Phase 6 — Cleanup + post-mortem

Required before declaring done:

- [ ] Original repro no longer reproduces (re-run the Phase 1 loop)
- [ ] Regression test passes (or absence of seam is documented)
- [ ] All `[DEBUG-...]` instrumentation removed (`grep` the prefix)
- [ ] Throwaway prototypes deleted (or moved to a clearly-marked debug location)
- [ ] The hypothesis that turned out correct is stated in the commit / PR message — so the next debugger learns

**Then ask: what would have prevented this bug?** Make architecture recommendations
only after confirming and fixing the root cause.

Invoke `$code-structure` only when the evidence shows its actual trigger: duplicated
operational mechanics across callers, or a module or package boundary that must
change to create a stable test seam. Pass it the minimal reproduction, causal chain,
affected callers, and the seam the regression test needs. It is not a general
substitute for an architecture review.

For other architectural causes such as tangled callers, hidden coupling, or a
missing test seam outside `$code-structure`'s narrow scope, report the concrete
finding and evidence without inventing a broad refactor or blocking completion on
an unavailable architecture skill.
