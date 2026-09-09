# Agent Workflow

Use this workflow for non-trivial implementation tasks in opencode.

1. Planning

Use `Deepseek V4 Pro` to create the implementation plan.
The plan must include scope, affected files, risks, verification steps,
and any open questions.

2. Plan Review

When the plan is ready, review it with `GPT-5.5` before any
implementation starts.

The review must check for correctness, missing edge cases, architectural
fit, test coverage, and unnecessary complexity.

3. User Approval

Do not build until the reviewed plan has been shown to the user and the
user explicitly approves it.

4. Build

After approval, implement the approved plan with
`Deepseek V4 Flash`.

Keep the implementation minimal, stay within the approved scope, and run
the verification steps from the plan before reporting completion.

5. Limits

This file is an instruction source, not a guaranteed model router. If
opencode cannot switch to one of the requested models in the current
session, say so clearly and ask the user how to proceed.
