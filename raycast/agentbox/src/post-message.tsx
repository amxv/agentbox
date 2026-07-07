import { Action, ActionPanel, Clipboard, Form, Icon, Toast, open, showToast } from "@raycast/api";
import { useMemo, useState } from "react";
import { Thread, createThread, dashboardThreadUrl, postMessage } from "./api";
import { BODY_FORMATS, FormValuesBase, normalizeFormError, uploadFilesForThread } from "./form-helpers";
import { AgentboxUtilityActions } from "./utility-actions";

type PostTarget = "existing" | "new";

type PostMessageValues = FormValuesBase & {
  target: PostTarget;
  threadId: string;
  title: string;
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
  const [target, setTarget] = useState<PostTarget>("existing");
  const [threadIdError, setThreadIdError] = useState<string | undefined>();
  const [titleError, setTitleError] = useState<string | undefined>();
  const [bodyError, setBodyError] = useState<string | undefined>();

  async function handleSubmit(values: PostMessageValues) {
    if (isLoading) {
      return false;
    }

    const selectedTarget = values.target;
    const threadId = (values.threadId ?? "").trim();
    const title = (values.title ?? "").trim();
    const body = values.body ?? "";
    const files = values.files ?? [];
    if (selectedTarget === "existing" && !threadId) {
      setThreadIdError("Thread ID is required.");
      return false;
    }
    if (selectedTarget === "new" && !title) {
      setTitleError("Title is required.");
      return false;
    }
    if (selectedTarget === "existing" && !body.trim() && files.length === 0) {
      setBodyError("Add a message or at least one attachment.");
      return false;
    }

    setIsLoading(true);
    setThreadIdError(undefined);
    setTitleError(undefined);
    setBodyError(undefined);
    const toast = await showToast({
      style: Toast.Style.Animated,
      title: selectedTarget === "new" ? "Creating thread" : "Posting message",
      message: selectedTarget === "new" ? title : threadId,
    });
    try {
      if (selectedTarget === "new") {
        const createdThread = await createThreadWithOptionalMessage({
          title,
          body,
          files,
          bodyFormat: values.bodyFormat,
        });
        setPostedThreadId(createdThread.id);
        toast.style = Toast.Style.Success;
        toast.title = "Created thread";
        toast.message = createdThread.title;
        toast.primaryAction = {
          title: "Open Thread",
          onAction: () => {
            void open(dashboardThreadUrl(createdThread.id));
          },
        };
        toast.secondaryAction = {
          title: "Copy Thread ID",
          onAction: () => {
            void Clipboard.copy(createdThread.id);
          },
        };
        return true;
      }

      const message = await postToExistingThread({ threadId, body, files, bodyFormat: values.bodyFormat });

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
      toast.title = selectedTarget === "new" ? "Could not create thread" : "Could not post message";
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
          <Action.SubmitForm
            title={target === "new" ? "Create Thread" : "Post Message"}
            icon={target === "new" ? Icon.Plus : Icon.Message}
            onSubmit={handleSubmit}
          />
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
      <Form.Dropdown
        id="target"
        title="Target"
        value={target}
        onChange={(value) => {
          setTarget(value as PostTarget);
          setThreadIdError(undefined);
          setTitleError(undefined);
          setBodyError(undefined);
        }}
      >
        <Form.Dropdown.Item value="existing" title="Existing Thread" />
        <Form.Dropdown.Item value="new" title="New Thread" />
      </Form.Dropdown>
      {target === "existing" ? (
        <Form.TextField
          id="threadId"
          title="Thread ID"
          placeholder="thr_..."
          defaultValue={initialThreadId}
          error={threadIdError}
        />
      ) : (
        <Form.TextField id="title" title="Title" placeholder="Daily agent handoff" error={titleError} />
      )}
      <Form.TextArea
        id="body"
        title={target === "new" ? "Initial Message" : "Message"}
        placeholder={
          target === "new"
            ? "Write the first message. Attachments can be posted with an empty body."
            : "Write a message for the thread. Attachments can be posted with an empty body."
        }
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

async function createThreadWithOptionalMessage({
  title,
  body,
  files,
  bodyFormat,
}: {
  title: string;
  body: string;
  files: string[];
  bodyFormat: PostMessageValues["bodyFormat"];
}): Promise<Thread> {
  const hasAttachments = files.length > 0;
  const created = await createThread({
    title,
    initialMessage: hasAttachments ? undefined : body || undefined,
    bodyContentType: hasAttachments || body ? bodyFormat : undefined,
  });

  if (hasAttachments) {
    await postToExistingThread({ threadId: created.thread.id, body, files, bodyFormat });
  }

  return created.thread;
}

async function postToExistingThread({
  threadId,
  body,
  files,
  bodyFormat,
}: {
  threadId: string;
  body: string;
  files: string[];
  bodyFormat: PostMessageValues["bodyFormat"];
}) {
  const uploadedAssets = await uploadFilesForThread(threadId, files);
  return postMessage({
    threadId,
    body,
    bodyContentType: bodyFormat,
    uploadedAssets,
  });
}
