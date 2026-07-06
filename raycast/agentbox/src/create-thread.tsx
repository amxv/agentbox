import { Action, ActionPanel, Clipboard, Form, Icon, Toast, open, showToast } from "@raycast/api";
import { useState } from "react";
import { Thread, createThread, dashboardThreadUrl, postMessage } from "./api";
import PostMessage from "./post-message";
import { BODY_FORMATS, FormValuesBase, normalizeFormError, uploadFilesForThread } from "./form-helpers";
import { AgentboxUtilityActions } from "./utility-actions";

type CreateThreadValues = FormValuesBase & {
  title: string;
  initialMessage: string;
};

export default function CreateThread() {
  const [isLoading, setIsLoading] = useState(false);
  const [createdThread, setCreatedThread] = useState<Thread | null>(null);
  const [error, setError] = useState<string | undefined>();

  async function handleSubmit(values: CreateThreadValues) {
    if (isLoading) {
      return false;
    }

    const title = values.title.trim();
    const initialMessage = values.initialMessage ?? "";
    const files = values.files ?? [];
    if (!title) {
      setError("Title is required.");
      return false;
    }

    setIsLoading(true);
    setError(undefined);
    const toast = await showToast({ style: Toast.Style.Animated, title: "Creating thread", message: title });
    try {
      const hasAttachments = files.length > 0;
      const created = await createThread({
        title,
        initialMessage: hasAttachments ? undefined : initialMessage || undefined,
        bodyContentType: hasAttachments || initialMessage ? values.bodyFormat : undefined,
      });

      if (hasAttachments) {
        const uploadedAssets = await uploadFilesForThread(created.thread.id, files);
        await postMessage({
          threadId: created.thread.id,
          body: initialMessage,
          bodyContentType: values.bodyFormat,
          uploadedAssets,
        });
      }

      setCreatedThread(created.thread);
      toast.style = Toast.Style.Success;
      toast.title = "Created thread";
      toast.message = created.thread.title;
      toast.primaryAction = {
        title: "Open Thread",
        onAction: () => {
          void open(dashboardThreadUrl(created.thread.id));
        },
      };
      toast.secondaryAction = {
        title: "Copy Thread ID",
        onAction: () => {
          void Clipboard.copy(created.thread.id);
        },
      };
      return true;
    } catch (submissionError) {
      const normalized = normalizeFormError(submissionError);
      setError(normalized.message);
      toast.style = Toast.Style.Failure;
      toast.title = "Could not create thread";
      toast.message = normalized.message;
      return false;
    } finally {
      setIsLoading(false);
    }
  }

  return (
    <Form
      enableDrafts
      isLoading={isLoading}
      actions={
        <ActionPanel>
          <Action.SubmitForm title="Create Thread" icon={Icon.Plus} onSubmit={handleSubmit} />
          {createdThread && (
            <ActionPanel.Section title="Created Thread">
              <Action.OpenInBrowser title="Open Thread" url={dashboardThreadUrl(createdThread.id)} icon={Icon.Globe} />
              <Action.CopyToClipboard title="Copy Thread URL" content={dashboardThreadUrl(createdThread.id)} />
              <Action.CopyToClipboard title="Copy Thread ID" content={createdThread.id} />
              <Action.Push
                title="Post Follow-Up"
                icon={Icon.Message}
                target={<PostMessage initialThreadId={createdThread.id} />}
              />
            </ActionPanel.Section>
          )}
          <AgentboxUtilityActions />
        </ActionPanel>
      }
    >
      <Form.TextField id="title" title="Title" placeholder="Daily agent handoff" error={error} />
      <Form.TextArea
        id="initialMessage"
        title="Initial Message"
        placeholder="Write the first message. Attachments can be posted with an empty body."
        enableMarkdown
      />
      <Form.Dropdown id="bodyFormat" title="Body Format" defaultValue="auto">
        {BODY_FORMATS.map((format) => (
          <Form.Dropdown.Item key={format.value} value={format.value} title={format.title} />
        ))}
      </Form.Dropdown>
      <Form.FilePicker
        id="files"
        title="Attachments"
        allowMultipleSelection
        canChooseDirectories={false}
        canChooseFiles
      />
    </Form>
  );
}
