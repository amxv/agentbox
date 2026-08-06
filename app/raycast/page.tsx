import type { Metadata } from "next";
import { RaycastView } from "./raycast-view";

export const metadata: Metadata = {
  title: "Agentbox for Raycast — The macOS seat at the shared inbox",
  description: "Browse, search, create, and update the same Agentbox threads used by humans and agents directly from Raycast on macOS."
};

export default function RaycastPage() {
  return <RaycastView />;
}
