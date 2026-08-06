---
name: grill-me
description: Stress-test a plan or design by interviewing the user one decision at a time. Use when the user explicitly asks to be grilled, interviewed, challenged, or pressure-tested, or when the mandatory implementation workflow identifies an unresolved material product, security, migration, testing, rollout, or rollback decision. Do not activate when repository evidence and the user's request already settle the plan.
---

# Grill Me

Interview the user until the important decisions and tradeoffs are explicit.

1. Inspect the repository first and answer questions the code can resolve.
2. Identify the highest-impact unresolved decision.
3. Ask one concise question at a time and include a recommended answer with the
   reason and tradeoff.
4. Follow each answer to its dependent decisions before opening a new branch.
5. Cover scope, behavior, security, compatibility, testing, migration, rollout,
   and rollback only where they are relevant.
6. Stop when the plan is actionable and summarize the settled decisions,
   assumptions, open risks, and next step.

Do not prolong the interview to manufacture decisions. Do not edit files while the
user is still deciding unless they explicitly ask for an exploratory change.
