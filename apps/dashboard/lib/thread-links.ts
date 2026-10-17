/** The ordinary HTTPS URL remains the reliable, shareable thread deep link. */
export function threadWebUrl(origin: string, threadId: string): string {
  const base = new URL(origin);
  if ((base.protocol !== "https:" && base.protocol !== "http:") || base.username || base.password) {
    throw new Error("Expected an HTTP(S) dashboard origin");
  }
  if (!threadId.trim()) throw new Error("Thread ID is required");
  return new URL(`/threads/${encodeURIComponent(threadId)}`, base).toString();
}

/**
 * Experimental iOS Home Screen web-app launcher.
 *
 * `webapp://` is an undocumented iOS system scheme, not a registered PWA
 * protocol handler. On some iOS versions it launches the installed app, but
 * may discard the thread path or fail outright. Never replace the HTTPS link
 * with this or promise that it opens the requested thread on iOS.
 */
export function experimentalIosPwaThreadUrl(origin: string, threadId: string): string {
  const url = new URL(threadWebUrl(origin, threadId));
  return `webapp://${url.host}${url.pathname}`;
}
