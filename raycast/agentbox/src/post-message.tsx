import { Action, ActionPanel, Form } from "@raycast/api";

export default function PostMessage() {
  return (
    <Form
      actions={
        <ActionPanel>
          <Action.SubmitForm title="Post Message" onSubmit={() => undefined} />
        </ActionPanel>
      }
    >
      <Form.TextField id="threadId" title="Thread ID" placeholder="thr_..." />
      <Form.TextArea id="body" title="Message" placeholder="Write a message for the thread" />
    </Form>
  );
}
