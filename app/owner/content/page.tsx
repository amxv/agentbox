import type { Metadata } from "next";
import { OwnerContentView } from "./owner-content-view";

export const metadata: Metadata = {
  title: "Owner content · Agentbox",
  robots: { index: false, follow: false }
};

export default function OwnerContentPage() {
  return <OwnerContentView />;
}
