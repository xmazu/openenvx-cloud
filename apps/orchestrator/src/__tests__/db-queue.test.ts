import { describe, test, beforeAll, afterAll, expect } from "bun:test";
import {
  PostgreSqlContainer,
  StartedPostgreSqlContainer,
} from "@testcontainers/postgresql";
import { drizzle } from "drizzle-orm/postgres-js";
import * as postgres from "postgres";
import { jobs } from "../db/schema.js";
import { fetchNextJob } from "../db/queue.js";
import { sql } from "drizzle-orm";

describe("Database Queue Concurrency", () => {
  let container: StartedPostgreSqlContainer;
  let client: postgres.Sql<any>;
  let db: any;

  beforeAll(async () => {
    console.log("Starting container...");
    container = await new PostgreSqlContainer("postgres:16-alpine").start();
    console.log("Container started");
    const uri = container.getConnectionUri();
    client = postgres.default(uri, { max: 10 });
    db = drizzle(client);

    console.log("Creating tables...");
    await db.execute(sql`
      CREATE TYPE job_status AS ENUM ('PENDING', 'DISPATCHED', 'COMPLETED', 'FAILED');
      CREATE TABLE jobs (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
        status text NOT NULL DEFAULT 'PENDING',
        payload jsonb NOT NULL,
        nomad_eval_id text,
        created_at timestamp NOT NULL DEFAULT NOW(),
        updated_at timestamp NOT NULL DEFAULT NOW()
      );
    `);
    console.log("Tables created");
  }, 120000);

  afterAll(async () => {
    if (client) await client.end();
    if (container) await container.stop();
  });

  test("fetchNextJob should return exactly one job to one caller in concurrent requests", async () => {
    await db.insert(jobs).values({
      payload: { task: "test-job" },
    });

    const [result1, result2] = await Promise.all([
      fetchNextJob(db),
      fetchNextJob(db),
    ]);

    const successCount = [result1, result2].filter(Boolean).length;
    expect(successCount).toBe(1);

    const successfulJob = result1 || result2;
    expect(successfulJob).not.toBeNull();
    expect(successfulJob!.status).toBe("DISPATCHED");
    expect((successfulJob!.payload as any).task).toBe("test-job");
  });
});
