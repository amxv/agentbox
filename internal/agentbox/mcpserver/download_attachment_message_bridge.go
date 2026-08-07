package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const downloadAttachmentMessageBridgeURI = "ui://agentbox/download-resource-message-v1.html"
const downloadAttachmentMessageBridgeDomain = "https://agentbox.ashray.xyz"

// registerDownloadAttachmentMessageBridgeResource adds a tiny MCP Apps view
// for hosts that support ui/message. The canonical tool result remains the
// standard ResourceLink plus structured download_url. The view simply re-emits
// that ResourceLink as a user-message content block so the host can make the
// file part of the conversation instead of leaving it trapped in a tool result.
func registerDownloadAttachmentMessageBridgeResource(server *mcp.Server) {
	server.AddResource(&mcp.Resource{
		URI:         downloadAttachmentMessageBridgeURI,
		Name:        "agentbox-download-resource-message",
		Title:       "AgentBox attachment",
		Description: "Makes an AgentBox attachment resource available to the host conversation.",
		MIMEType:    "text/html;profile=mcp-app",
	}, func(_ context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI:      downloadAttachmentMessageBridgeURI,
			MIMEType: "text/html;profile=mcp-app",
			Text:     downloadAttachmentMessageBridgeHTML,
			Meta: mcp.Meta{
				"ui": map[string]any{
					"prefersBorder": true,
					"csp": map[string]any{
						"connectDomains":  []string{},
						"resourceDomains": []string{},
					},
					"domain": downloadAttachmentMessageBridgeDomain,
				},
				"openai/widgetPrefersBorder": true,
				"openai/widgetCSP": map[string]any{
					"connect_domains":  []string{},
					"resource_domains": []string{},
				},
				"openai/widgetDomain": downloadAttachmentMessageBridgeDomain,
			},
		}}}, nil
	})
}

const downloadAttachmentMessageBridgeHTML = `<!doctype html>
<html>
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width,initial-scale=1" />
  <title>AgentBox attachment</title>
  <style>
    :root { color-scheme: light dark; font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    body { margin: 0; padding: 12px; }
    .title { font-weight: 650; margin-bottom: 6px; }
    .status { font-size: 13px; opacity: .78; white-space: pre-wrap; overflow-wrap: anywhere; }
  </style>
</head>
<body>
  <div class="title">AgentBox attachment</div>
  <div class="status" id="status">Adding attachment to the conversation…</div>
  <script>
    (() => {
      const statusEl = document.getElementById("status");
      const OPENAI_SET_GLOBALS_EVENT = "openai:set_globals";
      const MCP_APPS_PROTOCOL_VERSION = "2026-01-26";
      let rpcCounter = 0;

      function waitForOpenAIGlobal(key, timeoutMs = 10000) {
        const current = window.openai?.[key];
        if (current !== undefined && current !== null) return Promise.resolve(current);

        return new Promise((resolve) => {
          let settled = false;
          const finish = (value) => {
            if (settled) return;
            settled = true;
            window.removeEventListener(OPENAI_SET_GLOBALS_EVENT, onGlobals);
            clearTimeout(timeout);
            resolve(value ?? null);
          };
          const onGlobals = (event) => {
            const value = event?.detail?.globals?.[key];
            if (value !== undefined && value !== null) finish(value);
          };
          const timeout = setTimeout(() => finish(window.openai?.[key] ?? null), timeoutMs);
          window.addEventListener(OPENAI_SET_GLOBALS_EVENT, onGlobals, { passive: true });

          const afterSubscribe = window.openai?.[key];
          if (afterSubscribe !== undefined && afterSubscribe !== null) finish(afterSubscribe);
        });
      }

      function safeError(error) {
        return String(error?.message ?? error ?? "Unknown error").slice(0, 300);
      }

      function notify(method, params) {
        const message = { jsonrpc: "2.0", method };
        if (params !== undefined) message.params = params;
        window.parent.postMessage(message, "*");
      }

      function rpc(method, params, timeoutMs = 8000) {
        const id = "agentbox-" + Date.now() + "-" + (++rpcCounter);
        return new Promise((resolve, reject) => {
          let settled = false;
          const finish = (fn, value) => {
            if (settled) return;
            settled = true;
            window.removeEventListener("message", onMessage);
            clearTimeout(timeout);
            fn(value);
          };
          const onMessage = (event) => {
            if (event.source !== window.parent) return;
            const message = event.data;
            if (!message || message.jsonrpc !== "2.0" || message.id !== id) return;
            if (message.error) {
              finish(reject, new Error(message.error.message || "Host rejected MCP Apps request."));
              return;
            }
            finish(resolve, message.result ?? {});
          };
          const timeout = setTimeout(() => finish(reject, new Error(method + " timed out.")), timeoutMs);
          window.addEventListener("message", onMessage);
          window.parent.postMessage({ jsonrpc: "2.0", id, method, params }, "*");
        });
      }

      async function initializeBridge() {
        const initialized = await rpc("ui/initialize", {
          appInfo: { name: "agentbox-attachment-message", version: "1.0.0" },
          appCapabilities: {},
          protocolVersion: MCP_APPS_PROTOCOL_VERSION,
        });
        notify("ui/notifications/initialized");
        return initialized;
      }

      async function run() {
        if (window.__agentboxResourceMessageSent) return;
        window.__agentboxResourceMessageSent = true;

        const output = await waitForOpenAIGlobal("toolOutput");
        const asset = output?.asset ?? {};
        const downloadUrl = output?.download_url;
        if (!downloadUrl || !asset?.file_name) {
          throw new Error("AgentBox tool output is missing the attachment download resource.");
        }

        const initialized = await initializeBridge();
        if (!initialized?.hostCapabilities?.message?.resourceLink) {
          throw new Error("This host does not advertise ui/message ResourceLink support.");
        }

        const resourceLink = {
          type: "resource_link",
          uri: downloadUrl,
          name: asset.file_name,
          title: asset.file_name,
          mimeType: asset.mime_type || undefined,
          size: asset.size_bytes,
        };

        const result = await rpc("ui/message", {
          role: "user",
          content: [
            {
              type: "text",
              text: "AgentBox attachment " + JSON.stringify(asset.file_name) + " is ready. Use the attached ResourceLink directly; do not call download_attachment again unless this resource expires.",
            },
            resourceLink,
          ],
        });
        if (result?.isError) throw new Error("Host rejected the attachment message.");

        statusEl.textContent = asset.file_name + " was added to the conversation.";
      }

      run().catch((error) => {
        statusEl.textContent = "Could not add the AgentBox attachment to the conversation: " + safeError(error);
      });
    })();
  </script>
</body>
</html>`
