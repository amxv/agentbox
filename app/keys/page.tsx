import type { Metadata } from "next";
import { KeysView } from "./keys-view";

export const metadata: Metadata = {
  title: "Agentbox Keys",
  description: "Manage Agentbox API keys."
};

export default function KeysPage() {
  return <KeysView />;
}
