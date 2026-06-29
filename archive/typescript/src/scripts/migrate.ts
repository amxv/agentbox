import { closeDb, ensureSchema } from "../core/db";

try {
  await ensureSchema();
  console.log("Agentbox database schema is up to date.");
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
} finally {
  await closeDb();
}
