import type { Metadata } from "next";
import { OwnerUsersView } from "./owner-users-view";

export const metadata: Metadata = {
  title: "Users · Agentbox owner",
  description: "Invite, enable, and disable deployment-global Agentbox users."
};

export default function OwnerUsersPage() {
  return <OwnerUsersView />;
}
