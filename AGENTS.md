# OpenEnvX Cloud - AI Agent Guidelines

## Project Overview

OpenEnvX Cloud manages a **Nomad control plane** for workload orchestration.

### Architecture

- **Orchestrator Daemon**: A standalone `systemd` service running directly on the host VPS. It serves as the absolute source of truth for cluster state.
- **Terraform Workers**: Executed as parameterized batch jobs _inside_ the Nomad cluster, dispatched programmatically by the Orchestrator.
- **Job Management**: All Nomad job templates and updates must be managed via the Orchestrator—never manually via the Nomad CLI.

## Catalogs

| Path       | Description                                                        |
| ---------- | ------------------------------------------------------------------ |
| `nomad/`   | Nomad HCL job definitions and Docker plugin configs for local dev. |
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

- This repository manages a Nomad control plane.
- The **Orchestrator daemon** is a standalone `systemd` service on the host VPS and the absolute source of truth for the cluster.
- **Terraform workers** execute as parameterized batch jobs _inside_ the Nomad cluster, dispatched by the Orchestrator.
- Local development uses `go run cmd/orchestrator/main.go` for the control plane instead of `docker-compose`.
- All Nomad job templates and updates must be managed programmatically via the Orchestrator, not manually via the Nomad CLI.
