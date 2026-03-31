import { pgTable, text, timestamp, jsonb, uuid } from "drizzle-orm/pg-core";

export const jobStatusEnum = [
  "PENDING",
  "DISPATCHED",
  "RUNNING",
  "COMPLETED",
  "FAILED",
] as const;
export type JobStatus = (typeof jobStatusEnum)[number];

export interface JobPayload {
  task: string;
  [key: string]: any;
}

export const jobs = pgTable("jobs", {
  id: uuid("id").defaultRandom().primaryKey(),
  status: text("status", { enum: jobStatusEnum }).notNull().default("PENDING"),
  payload: jsonb("payload").$type<JobPayload>().notNull(),
  nomad_eval_id: text("nomad_eval_id"),
  created_at: timestamp("created_at").defaultNow().notNull(),
  updated_at: timestamp("updated_at").defaultNow().notNull(),
});
