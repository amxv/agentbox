import type { Metadata } from "next";
import { OwnerUsersView } from "./owner-users-view";

export const metadata: Metadata = {
  title: "Users & teams · Agentbox owner",
  description: "Manage deployment-global Agentbox users, teams, memberships, and invitations."
};

export default function OwnerUsersPage() {
  return <OwnerUsersView />;
}
