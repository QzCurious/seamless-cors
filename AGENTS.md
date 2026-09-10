## Agent skills

### Issue tracker

Issues are tracked as local markdown files under `.scratch/`. See `docs/agents/issue-tracker.md`.

### Triage labels

The repo uses the default five-label triage vocabulary. See `docs/agents/triage-labels.md`.

### Domain docs

This is a single-context repo using root-level `CONTEXT.md` and `docs/adr/`. See `docs/agents/domain.md`.

### Tests

When adding, reviewing, or pruning tests, apply the regression-signal policy in `docs/agents/testing.md`.

---

The project is still in development, we prefer a clean break over adding fallbacks or preserving backward compatibility.

Preallocate when obvious; reuse when ownership makes it natural; optimize aggressively only when performance actually matters.

Prefer derived state over synchronized redundant state.

Prefer organizing a sequential operation into inline phases separated by short,
intent-focused comments before extracting helper functions. Extract a helper when
it provides reuse or encapsulates a distinct responsibility that is easier to
understand independently.

Prefer constructing structs in their final form with a complete literal on each
return path when the fields are already known. Use incremental construction when
the algorithm naturally accumulates state. Error paths may return partial results
when those results carry useful information for callers.

## Ownership conventions

Do not defensively copy slices, maps, or pointers across internal boundaries
unless mutation is part of the contract or a concrete aliasing bug requires
isolation. Treat published values as read-only by convention; passing a slice
does not imply permission to mutate it.

## Error modeling

Prefer distinct concrete error types for failures with different semantic
meanings or caller consequences. Callers should branch only on distinctions
that change their behavior; a general non-nil error covers shared consequences.

Error-producing modules expose concrete facts and ordinary cause wrapping.
Callers classify those errors into caller-owned semantic state, own the
resulting behavior, and compare that state when deduplication is required.
