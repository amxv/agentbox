import { afterAll, beforeAll, describe, expect, test } from "bun:test";
import { createServer } from "node:http";

let backend;
let authenticatedDownload;
let authenticatedPreview;
let ownerDownload;
let ownerPreview;

beforeAll(async () => {
  backend = createServer((request, response) => {
    response.setHeader("content-type", "application/json");
    response.end(JSON.stringify({ path: request.url }));
  });
  await new Promise((resolve) => backend.listen(0, "127.0.0.1", resolve));
  const address = backend.address();
  process.env.AGENTBOX_BACKEND_URL = `http://127.0.0.1:${address.port}`;

  authenticatedDownload = await import("../../../apps/dashboard/app/api/assets/[assetId]/download-url/route.ts");
  authenticatedPreview = await import("../../../apps/dashboard/app/api/assets/[assetId]/preview-url/route.ts");
  ownerDownload = await import("../../../apps/dashboard/app/api/owner/content/assets/[assetId]/download/route.ts");
  ownerPreview = await import("../../../apps/dashboard/app/api/owner/content/assets/[assetId]/preview/route.ts");
});

afterAll(() => {
  delete process.env.AGENTBOX_BACKEND_URL;
  backend?.close();
});

async function proxiedPath(route, dashboardPath) {
  const request = new Request(`https://dashboard.example${dashboardPath}`);
  const response = await route.GET(request, { params: Promise.resolve({ assetId: "asset one" }) });
  expect(response.status).toBe(200);
  return (await response.json()).path;
}

describe("attachment resolution proxies", () => {
  test("authenticated download and preview preserve encoding and expiry", async () => {
    expect(await proxiedPath(authenticatedDownload, "/api/assets/asset%20one/download-url?expires_in=600"))
      .toBe("/api/assets/asset%20one/download-url?expires_in=600");
    expect(await proxiedPath(authenticatedPreview, "/api/assets/asset%20one/preview-url?expires_in=900"))
      .toBe("/api/assets/asset%20one/preview-url?expires_in=900");
  });

  test("owner download and preview preserve encoding and expiry", async () => {
    expect(await proxiedPath(ownerDownload, "/api/owner/content/assets/asset%20one/download?expires_in=600"))
      .toBe("/api/owner/content/assets/asset%20one/download?expires_in=600");
    expect(await proxiedPath(ownerPreview, "/api/owner/content/assets/asset%20one/preview?expires_in=900"))
      .toBe("/api/owner/content/assets/asset%20one/preview?expires_in=900");
  });
});
