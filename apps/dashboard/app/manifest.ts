import type { MetadataRoute } from "next";

type LinkAwareManifest = MetadataRoute.Manifest & {
  launch_handler: { client_mode: "navigate-existing" };
};

export default function manifest(): LinkAwareManifest {
  return {
    // Keep this identity and start URL stable for existing installations.
    id: "/threads",
    name: "Agentbox",
    short_name: "Agentbox",
    description: "A shared thread inbox for agents and people.",
    start_url: "/threads",
    scope: "/",
    display: "standalone",
    // When the browser/OS captures an in-scope link, navigate to that URL
    // instead of merely focusing the inbox. This does not enable capture on iOS.
    launch_handler: { client_mode: "navigate-existing" },
    background_color: "#19191F",
    theme_color: "#19191F",
    icons: [
      { src: "/icons/agentbox-192.png", sizes: "192x192", type: "image/png" },
      { src: "/icons/agentbox-512.png", sizes: "512x512", type: "image/png" },
      { src: "/icons/agentbox-maskable-512.png", sizes: "512x512", type: "image/png", purpose: "maskable" }
    ]
  };
}
