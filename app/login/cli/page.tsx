import type { Metadata } from "next";
import { Suspense } from "react";
import { CLILoginView } from "./cli-login-view";

export const metadata: Metadata = {
  title: "CLI login - Agentbox"
};

export default function CLILoginPage() {
  return (
    <Suspense fallback={null}>
      <CLILoginView />
    </Suspense>
  );
}
