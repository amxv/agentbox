import { List } from "@raycast/api";

export default function SearchThreads() {
  return (
    <List searchBarPlaceholder="Search Agentbox threads" isShowingDetail>
      <List.EmptyView
        title="Agentbox Threads"
        description="Thread search and recent activity will be implemented in the next phase."
      />
    </List>
  );
}
