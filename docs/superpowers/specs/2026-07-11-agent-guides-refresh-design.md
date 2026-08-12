# Agent Guides Refresh Design

## Goal

Refresh CamStation project guidance and analyze current project state without changing product behavior.

## Scope

- Analyze architecture, implementation state, code organization, safety boundaries, and verification health.
- Refresh root `AGENTS.md` plus guides under `cmd/camstationd`, `internal`, `internal/store`, `web`, `scripts`, and `docs/superpowers`.
- Preserve unrelated working-tree changes.
- Do not modify runtime data, generated configuration, secrets, or product code.

## Information Hierarchy

Root `AGENTS.md` owns project-wide facts:

- product shape and source-of-truth rules
- repository map and task routing
- global safety constraints
- shared verification commands
- runtime and documentation conventions

Nested guides own only local facts:

- directory purpose and boundaries
- local task-to-file routing
- local conventions and risks
- focused verification commands

Nested guides must not repeat broad project context unless needed to explain a local constraint.

## Refresh Rules

- Derive facts from current code, build files, recent commits, and maintained docs.
- Remove generated timestamps and commit hashes that become stale immediately.
- Distinguish implemented behavior from planned behavior.
- Keep instructions concise and operational.
- Retain security, credential-redaction, process-lifecycle, recording-deletion, and generated-file protections.
- Mention dirty-worktree handling and generated frontend asset workflow explicitly.
- Use KST for user-facing runtime timestamp explanations.

## Project Analysis Output

Final report covers:

- architecture and component boundaries
- implemented strengths
- incomplete or stale areas
- operational and security risks
- test/build evidence
- prioritized next actions
- exact guide files changed

Claims must cite repository files or fresh command output. Test or build success is reported only after commands run successfully.

## Verification

- Review all guide paths and referenced files for existence.
- Scan guides for stale commit metadata, contradictions, duplicated global rules, and unsafe instructions.
- Inspect final diff to ensure only intended documentation changed.
- Run documentation-relevant static checks; run project tests/builds to establish current health without modifying product source.

## Non-Goals

- No product feature implementation.
- No rewrite of product status documents unless required to correct an agent instruction.
- No daemon restart or real-camera mutation.
- No commit of user work or generated/runtime artifacts.
