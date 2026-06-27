import type { Metadata } from "next";
import { InboxView } from "./inbox-view";

export const metadata: Metadata = {
  title: "Agentbox Inbox",
  description: "Read-only Agentbox thread viewer."
};

export default function ThreadsPage() {
  return <InboxView />;
}
