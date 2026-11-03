function dashboardOrigin(origin: string): URL {
  const base = new URL(origin);
  if ((base.protocol !== "https:" && base.protocol !== "http:") || base.username || base.password ||
    base.pathname !== "/" || base.search || base.hash) {
    throw new Error("Expected an HTTP(S) dashboard origin without credentials or a path");
  }
  return base;
}

/** This same-origin HTTPS URL is the only reliable, shareable thread deep link. */
export function threadWebUrl(origin: string, threadId: string): string {
  if (!threadId.trim()) throw new Error("Thread ID is required");
  return new URL(`/threads/${encodeURIComponent(threadId)}`, dashboardOrigin(origin)).toString();
}

/**
 * Experimental iOS Home Screen app launcher; NOT a thread deep link.
 *
 * `webapp://` is an undocumented iOS system scheme, not a registered PWA
 * protocol handler. It may launch the installed app on some iOS versions,
 * but route and query parameters are not reliably honored, and it may fail.
 * Deliberately omit the thread path instead of suggesting it can be opened.
 */
export function experimentalIosPwaLaunchUrl(origin: string): string {
  return `webapp://${dashboardOrigin(origin).host}`;
}
