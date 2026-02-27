---
name: update-roadmap
description: Propose roadmap updates by integrating a coherent slice of the BACKLOG into existing (or new) ROADMAP phases, cross-checking ISSUES/DECISIONS/README for alignment. Report-first with easy partial approval.
---

# Update roadmap (integrate backlog into phases)

## Intent
Treat:
- `BACKLOG.md` as the **unscheduled backlog** of user-visible features and tasks.
- `ROADMAP.md` as the **source of truth for sequencing** and planned work.

This workflow **does not implement backlog items**. It produces a **reviewable proposal** for how to schedule backlog items into the roadmap, with explicit options for partial approval.

It also checks `ISSUES.md` because scheduling a forthcoming backlog item can:
- make an issue obsolete,
- turn an issue into a prerequisite,
- or reveal a missing issue that should exist.

---

## Optional guidance from the user
If the user provides extra direction, interpret it as:

- This workflow is proposal-only; it does not implement changes.
- Maximum new phases to propose (default: 2).
- Focus preference: a specific phase number, area, priority level, or text query; otherwise choose automatically.
- Proposal size: small (fewer suggestions), medium (balanced), or large (broader but still reviewable). Default is medium.
- Whether to include backlog entry identifiers (date/id/title) in the proposal (default: yes).
- Whether to include issue impact notes (default: yes).
- Phase renumbering behavior: incomplete phases may be renumbered when inserting or rearranging work; completed phases must not be renumbered.

---

## Roles and handoffs (multi-agent)
1. **Standards Reader**: reads `ROADMAP`, `DECISIONS`, `README` to extract constraints and direction.
2. **Backlog Triage Lead**: reads `BACKLOG` and ranks/filters candidates; validates “backlog item vs issue” classification.
3. **Issue Cross-Checker**: scans `ISSUES` and maps issue impacts (obsolete / prerequisite / unchanged).
4. **Roadmap Integrator**: produces grouped scheduling suggestions in the required format.
5. **Reviewer UX**: produces an “easy approval” response format and stops.

If only one agent is available, execute phases in this order with explicit headings.

---

## Guardrails
- This is **not** a code change workflow.
- Do not propose scheduling hundreds of items. The proposal must remain reviewable.
- Prefer a small number of **coherent groupings** over many scattered suggestions.
- Do not invent new requirements. Use only what is in BACKLOG/ISSUES/ROADMAP/README/DECISIONS.
- Do not schedule non-user-visible work from `BACKLOG.md`. If an entry is really an engineering improvement, recommend moving it to `ISSUES.md` instead.

---

# Phase 0 — Preflight (Standards Reader)

1. Ensure the project memory files exist:
   - `ROADMAP.md`
   - `BACKLOG.md`
   - `ISSUES.md`
   - `DECISIONS.md`

If any are missing:
- Ask the user before creating them. If approved, copy `.agent-layer/templates/docs/<NAME>.md` into `<NAME>.md` when available, preserving headings and markers.
- If a template is not available, create a minimal file with a clear purpose header and an entries section.

2. Read in this order (when present):
- `ROADMAP.md`
- `DECISIONS.md`
- `README.md`
- `BACKLOG.md`
- `ISSUES.md`

Extract:
- current phases (done vs not done)
- near-term roadmap direction (which phase(s) are upcoming)
- architectural constraints and “must avoid” rules
- any decisions that constrain scheduling (dependencies, chosen stack)

---

# Phase 1 — Backlog triage (Backlog Triage Lead)

## 1A) Parse and normalize the backlog
From `BACKLOG.md`, extract each backlog item’s:
- priority (Critical/High/Medium/Low)
- area
- capability
- acceptance criteria
- notes/dependencies (if any)

## 1B) Validate classification
Identify entries that do **not** appear user-visible (examples: refactors, test harness only, CI changes).
For each misclassified entry, prepare a “Recommendation” section:
- “Move to `ISSUES.md` as engineering work” (do not schedule in roadmap as a backlog item).

## 1C) Candidate selection (reviewable but adaptive)
Select a **reasonable** subset using these heuristics:

1. **Roadmap alignment first**
   - Prefer backlog items that naturally fit the next incomplete roadmap phase(s).
2. **Priority second**
   - Prefer Critical and High items.
3. **Cohesion third**
   - Prefer clusters that share a module/area and can be scheduled together.
4. **Dependencies**
   - If a backlog item depends on other work, include the smallest required set or explicitly mark the prerequisite.

### Proposal size heuristic
- If the user requests a small proposal, produce ~2–4 suggestions.
- If the user requests a medium proposal, produce ~3–6 suggestions.
- If the user requests a large proposal, produce ~5–8 suggestions.

Within a single suggestion, you may include more backlog items **if they are strongly related** and clearly belong together.

---

# Phase 2 — Cross-check ISSUES for impacts (Issue Cross-Checker)

If the user wants issue impacts included:

1. Scan `ISSUES.md` for issues related to selected candidate backlog items.
2. For each candidate backlog group, classify issue impact:
- **Obsoleted**: implementing the backlog item would likely remove the need for the issue.
- **Prerequisite**: the issue likely must be resolved before the backlog item is feasible.
- **Related**: overlaps but is not clearly resolved by the backlog item.
- **Unrelated**: ignore.

3. Record impacts as part of each proposal group so the user can decide:
- whether to keep, rewrite, or remove the issue once the backlog item is scheduled/implemented.

---

# Phase 3 — Integrate into ROADMAP proposals (Roadmap Integrator)

## 3A) List the current phases
In the chat output, list all roadmap phases in order, with a short label:
- `Phase N ✅` or `Phase N` (incomplete)

This helps the user review placement quickly.

## 3B) Produce suggestions grouped by phase
Produce a set of suggestions labeled **A, B, C, ...**.

Each suggestion must be one of:

### Type 1 — Add backlog items to an existing phase
Example structure:
**A. Add to “Phase 3 — Build the front-end”**
- Backlog item ... (title)
- Backlog item ... (title)

Include:
- Why this grouping fits the phase (1–2 bullets)
- Dependencies/prerequisites (bullets; point to issues when applicable)
- If any issues may become obsolete: list them under “Issue impacts”

### Type 2 — Add a new phase (up to the new-phase cap)
You may propose a new phase when:
- there is no suitable existing phase,
- or the backlog items represent a coherent chunk that deserves its own sequencing boundary.

**Important: phase numbering**
- You may propose inserting new phases and renumbering **incomplete** phases as needed.
- You must not propose any change that would require renumbering a **completed** phase (✅). In that case, propose an alternative placement.

Example structure:
**B. Add a new phase: “Phase X — Add end-to-end tests”**
Placement proposal:
- Preferred placement: between Phase 4 and Phase 5
- Notes: Renumbering is allowed **only** for phases that are not complete. Completed phases (✅) must keep their numbers.

Backlog items to schedule into this phase:
- Backlog item ...
- Backlog item ...

Include:
- Goal (1–3 bullets)
- High-level tasks (bullets; not a full implementation plan)
- Exit criteria (bullets)

## 3C) Optional: propose “cleanup / code improvements” phase (careful)
Only do this if the roadmap would benefit from a sequencing boundary for quality work **and** those items are not user-visible features or tasks.
- Prefer logging those as Issues; do not treat them as backlog items.
- If proposed, keep it as a separate suggestion group and explicitly label it “engineering work”.

---

# Phase 4 — Present the proposal for partial approval (Reviewer UX)

## Required output format (in chat)
1. **Roadmap overview** (current phases, in order).
2. **Suggestions A, B, C, ...** each with:
   - target phase (existing or new)
   - list of backlog items (bullets)
   - brief rationale
   - dependencies
   - issue impacts (if enabled)
3. **Backlog hygiene notes**
   - any misclassified backlog items that should be issues
   - any duplicate backlog items discovered

## User response protocol (mandatory)
Tell the user to respond with one line per suggestion letter using:

- `A: 1` = Approve
- `A: 2` = Remove from plan and do not do
- `A: 3 <instruction>` = Other (user provides edits, for example “remove the second backlog item”)

Examples:
- `A: 1`
- `B: 3 remove the second backlog item`
- `C: 2`

**Stop after presenting the proposal.** Do not modify any files until the user responds.

---

## Definition of done
- A reviewable proposal exists that maps a coherent subset of backlog items into roadmap phases.
- The proposal includes issue impact notes where relevant.
- The user has clear instructions to approve all, approve subset, or request edits per suggestion.
