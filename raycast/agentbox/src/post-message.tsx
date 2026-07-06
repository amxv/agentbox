import { Action, ActionPanel, Clipboard, Form, Icon, Toast, open, showToast } from "@raycast/api";
import { useMemo, useState } from "react";
import { dashboardThreadUrl, postMessage } from "./api";
import { BODY_FORMATS, FormValuesBase, normalizeFormError, uploadFilesForThread } from "./form-helpers";
import { AgentboxUtilityActions } from "./utility-actions";

type PostMessageValues = FormValuesBase & {
  threadId: string;
  body: string;
};

type PostMessageProps = {
  initialThreadId?: string;
  launchContext?: {
    threadId?: string;
  };
  arguments?: {
    threadId?: string;
  };
};

export default function PostMessage(props: PostMessageProps) {
  const initialThreadId = useMemo(() => {
    return props.initialThreadId ?? props.launchContext?.threadId ?? props.arguments?.threadId ?? "";
  }, [props.arguments?.threadId, props.initialThreadId, props.launchContext?.threadId]);
  const [isLoading, setIsLoading] = useState(false);
  const [postedThreadId, setPostedThreadId] = useState<string | null>(initialThreadId || null);
  const [threadIdError, setThreadIdError] = useState<string | undefined>();
  const [bodyError, setBodyError] = useState<string | undefined>();

  async function handleSubmit(values: PostMessageValues) {
    if (isLoading) {
      return false;
    }

    const threadId = values.threadId.trim();
    const body = values.body ?? "";
    const files = values.files ?? [];
    if (!threadId) {
      setThreadIdError("Thread ID is required.");
      return false;
    }
    if (!body.trim() && files.length === 0) {
      setBodyError("Add a message or at least one attachment.");
      return false;
    }

    setIsLoading(true);
    setThreadIdError(undefined);
    setBodyError(undefined);
    const toast = await showToast({ style: Toast.Style.Animated, title: "Posting message", message: threadId });
    try {
      const uploadedAssets = await uploadFilesForThread(threadId, files);
      const message = await postMessage({
        threadId,
        body,
        bodyContentType: values.bodyFormat,
        uploadedAssets,
      });

      setPostedThreadId(threadId);
      toast.style = Toast.Style.Success;
      toast.title = "Posted message";
      toast.message = message.id;
      toast.primaryAction = {
        title: "Open Thread",
        onAction: () => {
          void open(dashboardThreadUrl(threadId));
        },
      };
      toast.secondaryAction = {
        title: "Copy Message ID",
        onAction: () => {
          void Clipboard.copy(message.id);
        },
      };
      return true;
    } catch (submissionError) {
      const normalized = normalizeFormError(submissionError);
      setBodyError(normalized.message);
      toast.style = Toast.Style.Failure;
      toast.title = "Could not post message";
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
          <Action.SubmitForm title="Post Message" icon={Icon.Message} onSubmit={handleSubmit} />
          {postedThreadId && (
            <ActionPanel.Section title="Thread">
              <Action.OpenInBrowser title="Open Thread" url={dashboardThreadUrl(postedThreadId)} icon={Icon.Globe} />
              <Action.CopyToClipboard title="Copy Thread URL" content={dashboardThreadUrl(postedThreadId)} />
              <Action.CopyToClipboard title="Copy Thread ID" content={postedThreadId} />
            </ActionPanel.Section>
          )}
          <AgentboxUtilityActions />
        </ActionPanel>
      }
    >
      <Form.TextField
        id="threadId"
        title="Thread ID"
        placeholder="thr_..."
        defaultValue={initialThreadId}
        error={threadIdError}
      />
      <Form.TextArea
        id="body"
        title="Message"
        placeholder="Write a message for the thread. Attachments can be posted with an empty body."
        enableMarkdown
        error={bodyError}
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
