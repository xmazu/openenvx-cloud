import { PostgresJsDatabase } from "drizzle-orm/postgres-js";
import { eq } from "drizzle-orm";
import { NomadClient } from "./nomad/client.js";
import { fetchNextJob } from "./db/queue.js";
import { jobs } from "./db/schema.js";

export class Daemon {
  private db: PostgresJsDatabase;
  private nomadClient: NomadClient;
  private intervalId?: NodeJS.Timeout;
  private isRunning: boolean = false;
  private pollIntervalMs: number;
  private isTickRunning: boolean = false;

  constructor(
    db: PostgresJsDatabase,
    nomadClient: NomadClient,
    options: { pollIntervalMs?: number } = {},
  ) {
    this.db = db;
    this.nomadClient = nomadClient;
    this.pollIntervalMs = options.pollIntervalMs ?? 5000;
  }

  public async tick(): Promise<void> {
    if (this.isTickRunning) return;
    this.isTickRunning = true;

    try {
      await Promise.all([this.phase1Dispatch(), this.phase2Monitor()]);
    } catch (error) {
      console.error("Error during daemon tick:", error);
    } finally {
      this.isTickRunning = false;
    }
  }

  private async phase1Dispatch(): Promise<void> {
    const job = await fetchNextJob(this.db);
    if (!job) return;

    try {
      const payload = job.payload as Record<string, any>;
      const { task, ...meta } = payload;
      const nomadPayload: Record<string, string> = {};
      for (const [key, value] of Object.entries(meta || {})) {
        nomadPayload[key] = String(value);
      }

      const { evalId } = await this.nomadClient.dispatchJob(task, nomadPayload);

      await this.db
        .update(jobs)
        .set({
          status: "RUNNING",
          nomad_eval_id: evalId,
          updated_at: new Date(),
        })
        .where(eq(jobs.id, job.id as string));
    } catch (error) {
      console.error(`Failed to dispatch job ${job.id}:`, error);
      await this.db
        .update(jobs)
        .set({
          status: "FAILED",
          updated_at: new Date(),
        })
        .where(eq(jobs.id, job.id as string));
    }
  }

  private async phase2Monitor(): Promise<void> {
    const runningJobs = await this.db
      .select()
      .from(jobs)
      .where(eq(jobs.status, "RUNNING"));

    for (const job of runningJobs) {
      if (!job.nomad_eval_id) continue;

      try {
        const status = await this.nomadClient.getEvaluationStatus(
          job.nomad_eval_id,
        );

        if (status === "complete") {
          await this.db
            .update(jobs)
            .set({
              status: "COMPLETED",
              updated_at: new Date(),
            })
            .where(eq(jobs.id, job.id as string));
        } else if (status === "failed" || status === "canceled") {
          await this.db
            .update(jobs)
            .set({
              status: "FAILED",
              updated_at: new Date(),
            })
            .where(eq(jobs.id, job.id as string));
        }
      } catch (error) {
        console.error(`Failed to get status for job ${job.id}:`, error);
      }
    }
  }

  public start(): void {
    if (this.isRunning) return;
    this.isRunning = true;
    this.intervalId = setInterval(() => {
      this.tick().catch(console.error);
    }, this.pollIntervalMs);
  }

  public stop(): void {
    this.isRunning = false;
    if (this.intervalId) {
      clearInterval(this.intervalId);
      this.intervalId = undefined;
    }
  }
}
