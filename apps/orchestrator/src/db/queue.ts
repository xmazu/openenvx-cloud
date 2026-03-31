import { sql, eq } from "drizzle-orm";
import { PostgresJsDatabase } from "drizzle-orm/postgres-js";
import { jobs } from "./schema.js";

export async function fetchNextJob(db: PostgresJsDatabase) {
  const result = await db.execute(sql`
    UPDATE jobs
    SET status = 'DISPATCHED', updated_at = NOW()
    WHERE id = (
      SELECT id FROM jobs
      WHERE status = 'PENDING'
      ORDER BY created_at ASC
      FOR UPDATE SKIP LOCKED
      LIMIT 1
    )
    RETURNING *;
  `);

  if (result.length === 0) {
    return null;
  }

  return result[0];
}
