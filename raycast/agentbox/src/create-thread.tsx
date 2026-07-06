import { Action, ActionPanel, Form } from "@raycast/api";

export default function CreateThread() {
  return (
    <Form
      actions={
        <ActionPanel>
          <Action.SubmitForm title="Create Thread" onSubmit={() => undefined} />
        </ActionPanel>
      }
    >
      <Form.TextField id="title" title="Title" placeholder="Daily agent handoff" />
      <Form.TextArea id="initialMessage" title="Initial Message" placeholder="Write the first message" />
    </Form>
  );
}
