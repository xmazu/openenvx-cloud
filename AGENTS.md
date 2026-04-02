# OpenEnvX Cloud - AI Agent Guidelines

## Project Overview

OpenEnvX Cloud manages the absolute source of truth for cluster state.

### Architecture

- **Orchestrator Daemon**: A standalone `systemd` service running directly on the host VPS. It serves as the absolute source of truth for state management.
- **Terraform Workers**: Executed as parameterized batch jobs, dispatched programmatically by the Orchestrator.
- **Job Management**: All job templates and updates must be managed via the Orchestrator.

## Catalogs

| Path       | Description                                                        |
| ---------- | ------------------------------------------------------------------ |
| `local/`   | Local development stack (docker-compose with MinIO, PostgreSQL).   |
| `scripts/` | Build/deployment helper scripts.                                   |

---

**NEVER write any line of code without the explicit approval of a plan.**

Before writing, modifying, or deleting any code, you must:

1. Assess the user's request and read relevant context.
2. Outline a detailed, step-by-step implementation plan.
3. Pause and wait for the user to explicitly approve the plan.
4. Only upon receiving explicit approval may you begin using `Edit`, `Write`, or `Bash` tools to modify the codebase.

### Architecture Context

- This repository manages the absolute source of truth for cluster state.
- The **Orchestrator daemon** is a standalone `systemd` service on the host VPS and the absolute source of truth for the cluster.
- **Terraform workers** execute as parameterized batch jobs, dispatched by the Orchestrator.
- Local development uses `go run cmd/orchestrator/main.go` for the control plane instead of `docker-compose`.
- All job templates and updates must be managed programmatically via the Orchestrator.
