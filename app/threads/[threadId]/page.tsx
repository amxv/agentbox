import type { Metadata } from "next";
import { ThreadView } from "./thread-view";

export const metadata: Metadata = {
  title: "Agentbox Thread",
  description: "Read-only Agentbox thread details."
};

type Props = {
  params: Promise<{ threadId: string }>;
};

export default async function ThreadPage({ params }: Props) {
  const { threadId } = await params;
  return <ThreadView threadId={threadId} />;
}
