# OpenEnvX Cloud - Testing Guide

This document outlines the local development setup and the end-to-end testing flow for the OpenEnvX Cloud Orchestrator.

## 1. Prerequisites

Ensure you have the following installed:

- **Go 1.22+**
- **Docker & Docker Compose**
- **Nomad CLI** (added to your PATH)

## 2. Local Infrastructure Setup

Follow these steps in separate terminal windows:

### Step 1: Start Infrastructure (PostgreSQL & MinIO)

```bash
make infra
# Or manually: docker compose -f local/docker-compose.yml up -d
```

### Step 2: Start Nomad Agent & Register Job

The Orchestrator dispatches jobs to Nomad, but the `terraform-worker` template must exist first.

```bash
make nomad
# Or manually:
# nomad agent -dev -config=nomad/dev.hcl &
# sleep 5 && nomad job run nomad/terraform-worker.hcl
```

### Step 3: Start the Orchestrator Daemon

```bash
make setup  # Ensures .env exists
make orchestrator
# Or manually: go run cmd/orchestrator/main.go
```

## 3. End-to-End Testing Flow (The "Whole Flow")

Since the API is protected, you must identify a valid `user_id` and `organization_id` to authenticate.

### A. Get Test Credentials

Query your local database to find a valid user/org pair:

```bash
docker exec -it cloud-db-1 psql -U postgres -d openenvx -c "SELECT user_id, organization_id FROM membership LIMIT 1;"
```

### B. Trigger a Job (Plan)

Use the IDs from the previous step as Basic Auth credentials (**Username** = `user_id`, **Password** = `organization_id`).

```bash
USER_ID="your_user_id"
ORG_ID="your_org_id"

curl -X POST http://localhost:8080/internal/api/v1/jobs \
  -u "${USER_ID}:${ORG_ID}" \
  -H "Content-Type: application/json" \
  -d '{
    "project_id": "test-proj",
    "operation": "plan",
    "module_name": "network-base",
    "variables": {"region": "us-east-1"}
  }'
```

- **Verify**: Open [http://localhost:4646](http://localhost:4646) (Nomad UI). You should see a `terraform-worker` job running.

### C. Approve the Job (Apply)

Once the job status is `PLANNED` (check via `GET /internal/api/v1/jobs/<ID>`), approve it:

```bash
curl -X POST http://localhost:8080/internal/api/v1/jobs/<JOB_ID>/approve \
  -u "${USER_ID}:${ORG_ID}"
```

- **Verify**: A new "Apply" execution will be dispatched in Nomad.

## 4. Useful Makefile Commands

- `make help`: Show all available commands.
- `make stop`: Tear down all local services.
- `make orchestrator`: Restart only the Go daemon.
