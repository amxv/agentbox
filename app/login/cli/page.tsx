import type { Metadata } from "next";
import { Suspense } from "react";
import { CLILoginView } from "./cli-login-view";
export const metadata: Metadata = { title: "Authorize Agentbox CLI", description: "Authorize a named CLI credential to act for your Agentbox user account." };
export default function CLILoginPage(){return <Suspense fallback={null}><CLILoginView/></Suspense>}
