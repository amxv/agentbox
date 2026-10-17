import assert from "node:assert/strict";
import test from "node:test";
import { experimentalIosPwaThreadUrl, threadWebUrl } from "./thread-links";

test("web thread URLs are HTTPS URLs into the dashboard", () => {
  assert.equal(threadWebUrl("https://agentbox.ashray.xyz", "thr_123"),
    "https://agentbox.ashray.xyz/threads/thr_123");
  assert.equal(threadWebUrl("https://agentbox.example", "thr with/slash"),
    "https://agentbox.example/threads/thr%20with%2Fslash");
});

test("experimental iOS PWA URLs have no credentials or web-only scheme", () => {
  assert.equal(experimentalIosPwaThreadUrl("https://agentbox.ashray.xyz", "thr_123"),
    "webapp://agentbox.ashray.xyz/threads/thr_123");
  assert.equal(experimentalIosPwaThreadUrl("https://agentbox.example", "thr with/slash"),
    "webapp://agentbox.example/threads/thr%20with%2Fslash");
});

test("link builders reject invalid or empty inputs", () => {
  assert.throws(() => threadWebUrl("javascript:alert(1)", "thr_1"));
  assert.throws(() => threadWebUrl("https://alice:password@example.com", "thr_1"));
  assert.throws(() => experimentalIosPwaThreadUrl("https://agentbox.example", "   "));
});
