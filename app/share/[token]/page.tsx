import type { Metadata } from "next";
import { PublicThreadView } from "./public-thread-view";

export const metadata: Metadata = {
  title: "Shared thread · Agentbox",
  description: "A read-only Agentbox thread shared by public link.",
  robots: { index: false, follow: false }
};

export default async function PublicThreadPage({ params }: { params: Promise<{ token: string }> }) {
  const { token } = await params;
  return <PublicThreadView token={token}/>;
}
