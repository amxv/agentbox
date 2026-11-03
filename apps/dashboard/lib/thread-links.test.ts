import assert from "node:assert/strict";
import test from "node:test";
import { experimentalIosPwaLaunchUrl, threadWebUrl } from "./thread-links";

test("web thread URLs are HTTPS URLs into the dashboard", () => {
  assert.equal(threadWebUrl("https://agentbox.ashray.xyz", "thr_123"),
    "https://agentbox.ashray.xyz/threads/thr_123");
  assert.equal(threadWebUrl("https://agentbox.example", "thr with/slash"),
    "https://agentbox.example/threads/thr%20with%2Fslash");
});

test("experimental iOS launcher does not pretend to preserve a thread path", () => {
  assert.equal(experimentalIosPwaLaunchUrl("https://agentbox.ashray.xyz"),
    "webapp://agentbox.ashray.xyz");
  assert.equal(experimentalIosPwaLaunchUrl("https://agentbox.example:8443"),
    "webapp://agentbox.example:8443");
});

test("link builders reject invalid or empty inputs", () => {
  assert.throws(() => threadWebUrl("javascript:alert(1)", "thr_1"));
  assert.throws(() => threadWebUrl("https://alice:password@example.com", "thr_1"));
  assert.throws(() => threadWebUrl("https://agentbox.example/other", "thr_1"));
  assert.throws(() => threadWebUrl("https://agentbox.example?redirect=foo", "thr_1"));
  assert.throws(() => threadWebUrl("https://agentbox.example", "   "));
  assert.throws(() => experimentalIosPwaLaunchUrl("https://alice:password@example.com"));
});
