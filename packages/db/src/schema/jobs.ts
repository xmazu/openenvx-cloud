import {
  jsonb,
  pgEnum,
  pgTable,
  text,
  timestamp,
  uuid,
} from 'drizzle-orm/pg-core';
import { projects } from './projects.js';

export const jobStatusEnum = pgEnum('job_status', [
  'PENDING_PLAN',
  'PLANNING',
  'PLANNED',
  'APPROVED',
  'APPLYING',
  'APPLIED',
  'DESTROYING',
  'DESTROYED',
  'FAILED',
  'CANCELLED',
]);

export const jobs = pgTable('jobs', {
  id: uuid('id').primaryKey().defaultRandom(),
  projectId: uuid('project_id')
    .notNull()
    .references(() => projects.id),
  status: jobStatusEnum('status').notNull().default('PENDING_PLAN'),
  operation: text('operation').notNull(),
  moduleName: text('module_name').notNull(),
  variables: jsonb('variables'),
  planOutputPath: text('plan_output_path'),
  planSummary: text('plan_summary'),
  createdAt: timestamp('created_at').notNull().defaultNow(),
  updatedAt: timestamp('updated_at').notNull().defaultNow(),
});
