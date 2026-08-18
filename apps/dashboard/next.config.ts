import type { NextConfig } from "next";

const backendUrl = process.env.AGENTBOX_BACKEND_URL ?? process.env.AGENTBOX_GO_BACKEND_URL;

const nextConfig: NextConfig = {
  async rewrites() {
    if (!backendUrl) return [];
    return [
      {
        source: "/api/:path*",
        destination: `${backendUrl.replace(/\/+$/, "")}/api/:path*`
      }
    ];
  }
};

export default nextConfig;
