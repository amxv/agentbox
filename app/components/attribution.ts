export function attributionLabel(userDisplayName?: string, actorName?: string, fallback?: string) {
  const user = userDisplayName?.trim();
  const actor = actorName?.trim();
  if (user && actor && user.toLowerCase() !== actor.toLowerCase()) return `${user} · ${actor}`;
  return user || actor || (fallback !== undefined && fallback !== "" ? fallback : "Agentbox user");
}
