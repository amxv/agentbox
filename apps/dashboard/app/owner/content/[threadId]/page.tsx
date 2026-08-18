import type { Metadata } from "next";
import { OwnerContentThreadView } from "./owner-content-thread-view";

export const metadata: Metadata = {
  title: "Owner thread view · Agentbox",
  robots: { index: false, follow: false }
};

export default async function OwnerContentThreadPage({ params }: { params: Promise<{ threadId: string }> }) {
  const { threadId } = await params;
  return <OwnerContentThreadView threadId={threadId} />;
}
