---
name: deep-debug-audit
description: Exhaustive codebase debugging and audit. Use when the user wants a thorough code review, deep debugging session, full trace analysis, or complete audit of their project. Triggers on requests to find all bugs, trace all logic, audit code quality, investigate broken features, or perform line-by-line code analysis. Covers function tracing, variable flow analysis, error path verification, concurrency hazards, resource leaks, dead code detection, and feature completeness checks.
---

# Deep Debug Audit Skill

When this skill is triggered, read and follow the full audit prompt located at:

```
/mnt/skills/user/deep-debug-audit/deep-debug-audit.md
```

If that path is unavailable, the user should place the `deep-debug-audit.md` file in their skills directory.

## Activation

This skill activates on any of the following:
- "audit this codebase"
- "find all bugs"
- "trace the logic"
- "deep debug"
- "line by line review"
- "what's broken"
- "full code review"
- "investigate all issues"
- Any request for exhaustive or thorough code analysis

## Behavior

1. Read every project file before producing any findings.
2. Follow the 7-phase analysis protocol defined in the audit prompt.
3. Classify every finding with the defined tag set.
4. Produce the final summary table.
5. If output limit is hit, state progress and resume seamlessly on next message.
