import type { Metadata } from "next";
import { Suspense } from "react";
import { OwnerSetupView } from "./owner-setup-view";

export const metadata: Metadata = {
  title: "Set up the Agentbox owner",
  description: "Create or recover the permanent deployment owner with a one-time operator-issued token."
};

export default function OwnerSetupPage() {
  return <Suspense fallback={null}><OwnerSetupView /></Suspense>;
}
