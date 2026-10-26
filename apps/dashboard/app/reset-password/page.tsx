import type { Metadata } from "next";
import { ResetPasswordView } from "./reset-password-view";

export const metadata: Metadata = {
  title: "Reset your Agentbox password",
  description: "Set a new password using the private one-time link from your deployment owner.",
  robots: { index: false, follow: false }
};

export default function ResetPasswordPage() {
  return <ResetPasswordView />;
}
