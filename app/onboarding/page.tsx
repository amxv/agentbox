import type { Metadata } from "next";
import { OnboardingView } from "./onboarding-view";

export const metadata: Metadata = {
  title: "Connect your agents · Agentbox",
  description: "Connect ChatGPT, Claude, and a local coding agent to your Agentbox identity."
};

export default function OnboardingPage() {
  return <OnboardingView/>;
}
