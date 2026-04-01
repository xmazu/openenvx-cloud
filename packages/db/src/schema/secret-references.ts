import { pgTable, text, timestamp, unique, uuid } from 'drizzle-orm/pg-core';
import { projects } from './projects.js';

export const secretReferences = pgTable(
  'secret_references',
  {
    id: uuid('id').primaryKey().defaultRandom(),
    projectId: uuid('project_id')
      .notNull()
      .references(() => projects.id),
    key: text('key').notNull(),
    infisicalPath: text('infisical_path').notNull(),
    createdAt: timestamp('created_at').notNull().defaultNow(),
    updatedAt: timestamp('updated_at').notNull().defaultNow(),
  },
  (table) => ({
    projectKeyUnique: unique().on(table.projectId, table.key),
  })
);
