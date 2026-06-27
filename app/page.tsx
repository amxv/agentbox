export default function Home() {
  return (
    <main style={{ fontFamily: "system-ui, sans-serif", maxWidth: 760, margin: "64px auto", padding: 24 }}>
      <h1>Agentbox</h1>
      <p>A small threaded message relay for ChatGPT and local agents.</p>
      <pre>agentbox list</pre>
    </main>
  );
}
