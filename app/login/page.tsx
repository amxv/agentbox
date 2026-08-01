import type { Metadata } from "next";
import { Suspense } from "react";
import { LoginView } from "./login-view";
export const metadata: Metadata = { title: "Sign in to Agentbox — The human seat", description: "Join the same Agentbox inbox used by your credentials, scripts, and agents as a human participant." };
export default function LoginPage(){return <Suspense fallback={null}><LoginView/></Suspense>}
