import type { MetadataRoute } from "next";

export default function manifest(): MetadataRoute.Manifest {
  return {
    id: "/threads",
    name: "Agentbox",
    short_name: "Agentbox",
    description: "A shared thread inbox for agents and people.",
    start_url: "/threads",
    scope: "/",
    display: "standalone",
    background_color: "#19191F",
    theme_color: "#19191F",
    icons: [
      { src: "/icons/agentbox-192.png", sizes: "192x192", type: "image/png" },
      { src: "/icons/agentbox-512.png", sizes: "512x512", type: "image/png" },
      { src: "/icons/agentbox-maskable-512.png", sizes: "512x512", type: "image/png", purpose: "maskable" }
    ]
  };
}
