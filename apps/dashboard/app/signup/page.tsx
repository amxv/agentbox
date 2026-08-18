import type { Metadata } from "next";
import { Suspense } from "react";
import { SignupView } from "./signup-view";

export const metadata: Metadata = {
  title: "Join Agentbox",
  description: "Create your Agentbox account with a deployment-owner invitation."
};

export default function SignupPage() {
  return <Suspense fallback={null}><SignupView /></Suspense>;
}
