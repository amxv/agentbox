import { expect, test } from "bun:test";
import { experimentalIosPwaThreadUrl, threadWebUrl } from "./thread-links";

test("web thread URLs are HTTPS URLs into the dashboard", () => {
  expect(threadWebUrl("https://agentbox.ashray.xyz", "thr_123"))
    .toBe("https://agentbox.ashray.xyz/threads/thr_123");
  expect(threadWebUrl("https://agentbox.example", "thr with/slash"))
    .toBe("https://agentbox.example/threads/thr%20with%2Fslash");
});

test("experimental iOS PWA URLs have no credentials or web-only scheme", () => {
  expect(experimentalIosPwaThreadUrl("https://agentbox.ashray.xyz", "thr_123"))
    .toBe("webapp://agentbox.ashray.xyz/threads/thr_123");
  expect(experimentalIosPwaThreadUrl("https://agentbox.example", "thr with/slash"))
    .toBe("webapp://agentbox.example/threads/thr%20with%2Fslash");
});

test("link builders reject invalid or empty inputs", () => {
  expect(() => threadWebUrl("javascript:alert(1)", "thr_1")).toThrow();
  expect(() => threadWebUrl("https://alice:password@example.com", "thr_1")).toThrow();
  expect(() => experimentalIosPwaThreadUrl("https://agentbox.example", "   ")).toThrow();
});
