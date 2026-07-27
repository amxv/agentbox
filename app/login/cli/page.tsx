import type { Metadata } from "next";
import { Suspense } from "react";
import { CLILoginView } from "./cli-login-view";
export const metadata: Metadata = { title: "Authorize Agentbox CLI", description: "Authorize a named CLI participant to join the same tenant-scoped Agentbox inbox." };
export default function CLILoginPage(){return <Suspense fallback={null}><CLILoginView/></Suspense>}
