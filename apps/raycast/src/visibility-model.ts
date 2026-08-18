import type { ManageThreadVisibilityInput, ManagedThreadVisibility, Team } from "./api-client";

export type VisibilitySelection = {
  selectedTeamIDs: string[];
  publicEnabled: boolean;
};

export function visibilityMutation(
  current: ManagedThreadVisibility,
  selection: VisibilitySelection,
): ManageThreadVisibilityInput {
  const currentTeamIDs = unique(current.shared_teams.map((team) => team.id));
  const nextTeamIDs = unique(selection.selectedTeamIDs);
  const currentSet = new Set(currentTeamIDs);
  const nextSet = new Set(nextTeamIDs);
  const input: ManageThreadVisibilityInput = {};

  const addTeams = nextTeamIDs.filter((teamID) => !currentSet.has(teamID));
  const removeTeams = currentTeamIDs.filter((teamID) => !nextSet.has(teamID));
  if (addTeams.length > 0) input.add_teams = addTeams;
  if (removeTeams.length > 0) input.remove_teams = removeTeams;
  if (selection.publicEnabled !== current.public) input.public = selection.publicEnabled;
  return input;
}

export function mutationHasChanges(input: ManageThreadVisibilityInput): boolean {
  return Boolean(
    input.public !== undefined ||
    input.regenerate_public_link ||
    (input.add_teams && input.add_teams.length > 0) ||
    (input.remove_teams && input.remove_teams.length > 0),
  );
}

export function wouldSelfRevoke({
  currentUserID,
  current,
  selectedTeamIDs,
}: {
  currentUserID: string;
  current: ManagedThreadVisibility;
  selectedTeamIDs: string[];
}): boolean {
  if (current.owner_user_id === currentUserID) return false;
  const callerTeamIDs = new Set(current.available_teams.map((team) => team.id));
  return !unique(selectedTeamIDs).some((teamID) => callerTeamIDs.has(teamID));
}

export function visibilityTeamOptions(current: ManagedThreadVisibility): Team[] {
  const byID = new Map<string, Team>();
  for (const team of [...current.shared_teams, ...current.available_teams]) byID.set(team.id, team);
  return Array.from(byID.values()).sort((left, right) => left.name.localeCompare(right.name));
}

function unique(values: string[]): string[] {
  return Array.from(new Set(values.map((value) => value.trim()).filter(Boolean)));
}
