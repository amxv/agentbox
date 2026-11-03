import assert from "node:assert/strict";
import test from "node:test";
import manifest from "../app/manifest";

test("existing installations keep their identity and inbox entry point", () => {
  const app = manifest();
  assert.equal(app.id, "/threads");
  assert.equal(app.start_url, "/threads");
  assert.equal(app.display, "standalone");
});

test("direct thread links are in scope and launch into their destination", () => {
  const app = manifest();
  const origin = "https://agentbox.ashray.xyz";
  assert.ok(app.scope);
  const scope = new URL(app.scope, origin);
  const destination = new URL("/threads/thr_123?source=notification", origin);

  assert.equal(scope.pathname, "/");
  assert.equal(destination.origin, scope.origin);
  assert.ok(destination.pathname.startsWith(scope.pathname));
  assert.deepEqual(app.launch_handler, { client_mode: "navigate-existing" });
});
