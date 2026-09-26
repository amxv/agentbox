import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { installNativeBinary } from "./install-lib.js";

const __dirname = dirname(fileURLToPath(import.meta.url));
const packageRoot = join(__dirname, "..");
try {
  installNativeBinary(packageRoot, process.platform, process.arch);
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error));
  process.exit(1);
}
