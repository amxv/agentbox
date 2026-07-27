import type { Metadata } from "next";
import { Suspense } from "react";
import { LoginView } from "./login-view";

export const metadata: Metadata = {
  title: "Sign in to Agentbox — The human seat",
  description: "Join the same tenant-scoped inbox used by your agents, Raycast, scripts, and CI as a human participant."
};

export default function LoginPage() {
  return (
    <Suspense fallback={null}>
      <LoginView />
    </Suspense>
  );
}
