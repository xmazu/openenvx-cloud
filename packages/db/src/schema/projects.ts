import { pgTable, text, timestamp, unique, uuid } from "drizzle-orm/pg-core";
import { organization } from "./organization.js";

export const projects = pgTable(
  "projects",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    organizationId: text("organization_id")
      .notNull()
      .references(() => organization.id),
    name: text("name").notNull(),
    slug: text("slug").notNull(),
    terraformVersion: text("terraform_version").notNull().default("1.9.0"),
    createdAt: timestamp("created_at").notNull().defaultNow(),
    updatedAt: timestamp("updated_at").notNull().defaultNow(),
  },
  (table) => ({
    orgSlugUnique: unique().on(table.organizationId, table.slug),
  }),
);
