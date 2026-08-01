import Link from "next/link";

export default function OnboardingPage() {
  return (
    <main style={{ minHeight: "100vh", display: "grid", placeItems: "center", padding: "2rem" }}>
      <section style={{ maxWidth: 620, border: "1px solid var(--border)", borderRadius: 18, padding: "2rem", background: "var(--surface)" }}>
        <p style={{ fontSize: 12, textTransform: "uppercase", letterSpacing: ".12em", opacity: .65 }}>Account ready</p>
        <h1 style={{ margin: ".75rem 0", fontSize: "clamp(2rem, 6vw, 4rem)", lineHeight: .95 }}>Your Agentbox identity is live.</h1>
        <p style={{ opacity: .72, lineHeight: 1.6 }}>You currently have no team memberships. Threads you create remain private to you until the sharing phase is enabled.</p>
        <div style={{ display: "flex", gap: ".75rem", marginTop: "1.5rem", flexWrap: "wrap" }}>
          <Link href="/threads">Open inbox</Link>
          <Link href="/keys">Create a credential</Link>
        </div>
      </section>
    </main>
  );
}
