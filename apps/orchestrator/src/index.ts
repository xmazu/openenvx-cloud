import { drizzle } from "drizzle-orm/postgres-js";
import postgres from "postgres";
import { NomadClient } from "./nomad/client.js";
import { Daemon } from "./daemon.js";

const databaseUrl = process.env.DATABASE_URL;
if (!databaseUrl) {
  console.error("DATABASE_URL environment variable is required.");
  process.exit(1);
}

const nomadUrl = process.env.NOMAD_URL || "http://localhost:4646";
const nomadToken = process.env.NOMAD_TOKEN;

const sql = postgres(databaseUrl);
const db = drizzle(sql);

const nomadClient = new NomadClient(nomadUrl, nomadToken);
const daemon = new Daemon(db, nomadClient);

console.log("Starting orchestrator daemon...");
daemon.start();

process.on("SIGINT", () => {
  console.log("Shutting down...");
  daemon.stop();
  sql.end().catch(console.error);
  process.exit(0);
});

process.on("SIGTERM", () => {
  console.log("Shutting down...");
  daemon.stop();
  sql.end().catch(console.error);
  process.exit(0);
});
