# OpenEnvX Cloud - AI Agent Guidelines


**NEVER write any line of code without the explicit approval of a plan.**

Before writing, modifying, or deleting any code, you must:

1. Assess the user's request and read relevant context.
2. Outline a detailed, step-by-step implementation plan.
3. Pause and wait for the user to explicitly approve the plan.
4. Only upon receiving explicit approval may you begin using `Edit`, `Write`, or `Bash` tools to modify the codebase.


### Architecture Context

- This repository manages a Nomad control plane.
- The `Orchestrator` daemon is the absolute source of truth for the cluster.
- All Nomad job templates and updates must be managed programmatically via the Orchestrator, not manually via the Nomad CLI.
