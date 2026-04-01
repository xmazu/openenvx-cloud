import { drizzle } from 'drizzle-orm/postgres-js';
import postgres from 'postgres';

const connectionString =
  process.env.DATABASE_URL ||
  'postgres://postgres:postgres@localhost:5432/openenvx';

const client = postgres(connectionString);

export const db = drizzle(client);

export type Database = typeof db;
