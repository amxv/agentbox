import {
  Action,
  ActionPanel,
  Alert,
  Detail,
  Form,
  Icon,
  Toast,
  confirmAlert,
  showToast,
  useNavigation,
} from "@raycast/api";
import { useCallback, useEffect, useMemo, useState } from "react";
import {
  AgentboxAPIError,
  AuthContext,
  ManagedThreadVisibility,
  ManageThreadVisibilityInput,
  authMe,
  getThreadVisibility,
  manageThreadVisibility,
} from "./api";
import { escapeMarkdown } from "./markdown";
import { mutationHasChanges, visibilityMutation, visibilityTeamOptions, wouldSelfRevoke } from "./visibility-model";

type ManageVisibilityProps = {
  threadId: string;
  threadTitle: string;
  onChanged: () => void;
  onSelfRevoked: (threadId: string) => void;
};

export default function ManageVisibility({ onChanged, onSelfRevoked, threadId, threadTitle }: ManageVisibilityProps) {
  const { pop } = useNavigation();
  const [auth, setAuth] = useState<AuthContext | null>(null);
  const [visibility, setVisibility] = useState<ManagedThreadVisibility | null>(null);
  const [selectedTeamIDs, setSelectedTeamIDs] = useState<string[]>([]);
  const [publicEnabled, setPublicEnabled] = useState(false);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);

  const load = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const [nextAuth, nextVisibility] = await Promise.all([authMe(), getThreadVisibility(threadId)]);
      setAuth(nextAuth);
      setVisibility(nextVisibility);
      setSelectedTeamIDs(nextVisibility.shared_teams.map((team) => team.id));
      setPublicEnabled(nextVisibility.public);
    } catch (loadError) {
      setError(normalizeError(loadError));
    } finally {
      setIsLoading(false);
    }
  }, [threadId]);

  useEffect(() => {
    void load();
  }, [load]);

  const teams = useMemo(() => (visibility ? visibilityTeamOptions(visibility) : []), [visibility]);
  const selfRevoking = Boolean(
    auth && visibility && wouldSelfRevoke({ currentUserID: auth.user_id, current: visibility, selectedTeamIDs }),
  );

  async function applyMutation(input: ManageThreadVisibilityInput, description: string, checkSelfRevocation: boolean) {
    if (!visibility || !auth || !mutationHasChanges(input)) return;
    const revokesCaller =
      checkSelfRevocation && wouldSelfRevoke({ currentUserID: auth.user_id, current: visibility, selectedTeamIDs });
    if (revokesCaller) {
      const confirmed = await confirmAlert({
        icon: Icon.Warning,
        title: "Remove your access to this thread?",
        message:
          "The selected teams no longer include any team that grants your user access. Public status does not keep the thread in your authenticated inbox. After saving, this Raycast credential will no longer be able to open or post to the thread.",
        primaryAction: { title: "Save and Remove My Access", style: Alert.ActionStyle.Destructive },
      });
      if (!confirmed) return;
    }

    setIsLoading(true);
    try {
      const updated = await manageThreadVisibility(threadId, input);
      setVisibility(updated);
      setSelectedTeamIDs(updated.shared_teams.map((team) => team.id));
      setPublicEnabled(updated.public);
      await showToast({ style: Toast.Style.Success, title: description });
      if (revokesCaller) {
        onSelfRevoked(threadId);
        pop();
      } else {
        onChanged();
      }
    } catch (mutationError) {
      await showToast({
        style: Toast.Style.Failure,
        title: "Could not update visibility",
        message: normalizeError(mutationError).message,
      });
    } finally {
      setIsLoading(false);
    }
  }

  async function save() {
    if (!visibility) return;
    const input = visibilityMutation(visibility, { selectedTeamIDs, publicEnabled });
    if (!mutationHasChanges(input)) {
      await showToast({ style: Toast.Style.Success, title: "Visibility is already up to date" });
      return;
    }
    await applyMutation(input, "Thread visibility updated", true);
  }

  async function regeneratePublicLink() {
    const confirmed = await confirmAlert({
      icon: Icon.RotateClockwise,
      title: "Regenerate the public link?",
      message: "The current read-only public URL will stop working immediately.",
      primaryAction: { title: "Regenerate Link", style: Alert.ActionStyle.Destructive },
    });
    if (!confirmed) return;
    await applyMutation({ regenerate_public_link: true }, "Public link regenerated", false);
  }

  async function revokePublicLink() {
    const confirmed = await confirmAlert({
      icon: Icon.XMarkCircle,
      title: "Disable the public link?",
      message: "The current read-only public URL will stop working immediately.",
      primaryAction: { title: "Disable Public Link", style: Alert.ActionStyle.Destructive },
    });
    if (!confirmed) return;
    await applyMutation({ public: false }, "Public link disabled", false);
  }

  if (error) {
    return (
      <Detail
        navigationTitle={`Visibility · ${threadTitle}`}
        markdown={`# Could not load visibility\n\n${escapeMarkdown(error.message)}`}
        actions={
          <ActionPanel>
            <Action title="Retry" icon={Icon.ArrowClockwise} onAction={() => void load()} />
          </ActionPanel>
        }
      />
    );
  }

  return (
    <Form
      isLoading={isLoading}
      navigationTitle={`Visibility · ${threadTitle}`}
      actions={
        <ActionPanel>
          <ActionPanel.Section>
            <Action.SubmitForm title="Save Visibility" icon={Icon.Checkmark} onSubmit={() => void save()} />
            <Action title="Reload Visibility" icon={Icon.ArrowClockwise} onAction={() => void load()} />
          </ActionPanel.Section>
          {visibility?.public && (
            <ActionPanel.Section title="Public Link">
              {visibility.public_url && (
                <>
                  <Action.OpenInBrowser title="Open Public Link" icon={Icon.Globe} url={visibility.public_url} />
                  <Action.CopyToClipboard title="Copy Public Link" icon={Icon.Link} content={visibility.public_url} />
                </>
              )}
              <Action
                title="Regenerate Public Link"
                icon={Icon.RotateClockwise}
                style={Action.Style.Destructive}
                onAction={() => void regeneratePublicLink()}
              />
              <Action
                title="Disable Public Link"
                icon={Icon.XMarkCircle}
                style={Action.Style.Destructive}
                onAction={() => void revokePublicLink()}
              />
            </ActionPanel.Section>
          )}
        </ActionPanel>
      }
    >
      <Form.Description
        title="Thread"
        text={
          auth && visibility?.owner_user_id === auth.user_id
            ? `${threadTitle} · You own this thread and cannot lose access.`
            : `${threadTitle} · Your access depends on at least one selected team you belong to.`
        }
      />
      <Form.TagPicker
        id="sharedTeams"
        title="Shared Teams"
        value={selectedTeamIDs}
        onChange={setSelectedTeamIDs}
        info="Everyone in a selected team has full participation rights."
      >
        {teams.map((team) => (
          <Form.TagPicker.Item key={team.id} value={team.id} title={team.name} />
        ))}
      </Form.TagPicker>
      {teams.length === 0 && (
        <Form.Description
          title="Teams"
          text="No team is available to this user. Saving an empty selection keeps an owned thread private or removes a non-owner's team access."
        />
      )}
      <Form.Checkbox
        id="public"
        title="Public Link"
        label="Enable a revocable, read-only public URL"
        value={publicEnabled}
        onChange={setPublicEnabled}
      />
      {visibility?.public_url && <Form.Description title="Current Public URL" text={visibility.public_url} />}
      {selfRevoking && (
        <Form.Description
          title="Access Warning"
          text="Saving this team selection removes your user's final authenticated access path. Public status does not preserve inbox access."
        />
      )}
    </Form>
  );
}

function normalizeError(error: unknown): Error {
  if (error instanceof AgentboxAPIError) return error;
  return error instanceof Error ? error : new Error(String(error));
}
