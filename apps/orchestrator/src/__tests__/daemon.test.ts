import {
  describe,
  test,
  beforeAll,
  afterAll,
  afterEach,
  expect,
} from "bun:test";
import {
  PostgreSqlContainer,
  StartedPostgreSqlContainer,
} from "@testcontainers/postgresql";
import { drizzle } from "drizzle-orm/postgres-js";
import * as postgres from "postgres";
import { sql, eq } from "drizzle-orm";
import { setupServer } from "msw/node";
import { http, HttpResponse } from "msw";

import { jobs } from "../db/schema.js";
import { NomadClient } from "../nomad/client.js";
import { Daemon } from "../daemon.js";

const NOMAD_URL = "http://localhost:4646";
const server = setupServer();

describe("Daemon", () => {
  let container: StartedPostgreSqlContainer;
  let client: postgres.Sql<any>;
  let db: any;
  let daemon: Daemon;
  let nomadClient: NomadClient;

  beforeAll(async () => {
    container = await new PostgreSqlContainer("postgres:16-alpine").start();
    const uri = container.getConnectionUri();
    client = postgres.default(uri, { max: 10 });
    db = drizzle(client);

    await db.execute(sql`
      CREATE TYPE job_status AS ENUM ('PENDING', 'DISPATCHED', 'RUNNING', 'COMPLETED', 'FAILED');
      CREATE TABLE jobs (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
        status text NOT NULL DEFAULT 'PENDING',
        payload jsonb NOT NULL,
        nomad_eval_id text,
        created_at timestamp NOT NULL DEFAULT NOW(),
        updated_at timestamp NOT NULL DEFAULT NOW()
      );
    `);

    server.listen({ onUnhandledRequest: "error" });

    nomadClient = new NomadClient(NOMAD_URL, "secret-token", {
      maxRetries: 0,
    });
    daemon = new Daemon(db, nomadClient, { pollIntervalMs: 1000 });
  }, 120000);

  afterEach(() => {
    server.resetHandlers();
  });

  afterAll(async () => {
    server.close();
    daemon.stop();
    if (client) await client.end();
    if (container) await container.stop();
  });

  test("full job lifecycle: PENDING -> RUNNING -> COMPLETED", async () => {
    const [insertedJob] = await db
      .insert(jobs)
      .values({
        payload: { task: "my-test-job", some_arg: "123" },
      })
      .returning();

    expect(insertedJob.status).toBe("PENDING");

    server.use(
      http.post(
        `${NOMAD_URL}/v1/job/my-test-job/dispatch`,
        async ({ request }) => {
          return HttpResponse.json({
            EvalID: "eval-999",
            EvalCreateIndex: 10,
            JobCreateIndex: 5,
            Index: 10,
          });
        },
      ),
    );

    await daemon.tick();

    const runningJob = await db
      .select()
      .from(jobs)
      .where(eq(jobs.id, insertedJob.id))
      .then((res: any) => res[0]);

    expect(runningJob.status).toBe("RUNNING");
    expect(runningJob.nomad_eval_id).toBe("eval-999");

    server.use(
      http.get(`${NOMAD_URL}/v1/evaluation/eval-999`, () => {
        return HttpResponse.json({
          ID: "eval-999",
          Status: "complete",
        });
      }),
    );

    await daemon.tick();

    const completedJob = await db
      .select()
      .from(jobs)
      .where(eq(jobs.id, insertedJob.id))
      .then((res: any) => res[0]);

    expect(completedJob.status).toBe("COMPLETED");
  });

  test("job lifecycle: PENDING -> RUNNING -> FAILED", async () => {
    const [insertedJob] = await db
      .insert(jobs)
      .values({
        payload: { task: "my-failing-job" },
      })
      .returning();

    server.use(
      http.post(`${NOMAD_URL}/v1/job/my-failing-job/dispatch`, async () => {
        return HttpResponse.json({
          EvalID: "eval-fail-888",
        });
      }),
    );

    await daemon.tick();

    const runningJob = await db
      .select()
      .from(jobs)
      .where(eq(jobs.id, insertedJob.id))
      .then((res: any) => res[0]);

    expect(runningJob.status).toBe("RUNNING");

    server.use(
      http.get(`${NOMAD_URL}/v1/evaluation/eval-fail-888`, () => {
        return HttpResponse.json({
          ID: "eval-fail-888",
          Status: "failed",
        });
      }),
    );

    await daemon.tick();

    const failedJob = await db
      .select()
      .from(jobs)
      .where(eq(jobs.id, insertedJob.id))
      .then((res: any) => res[0]);

    expect(failedJob.status).toBe("FAILED");
  });
});
