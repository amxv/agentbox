import { chmodSync, existsSync, readFileSync, writeFileSync } from "node:fs";

const path = "dist/index.js";
if (existsSync(path)) {
  const contents = readFileSync(path, "utf8");
  if (!contents.startsWith("#!/usr/bin/env node")) {
    writeFileSync(path, `#!/usr/bin/env node\n${contents}`);
  }
  chmodSync(path, 0o755);
}
