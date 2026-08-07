package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const downloadAttachmentMessageBridgeURI = "ui://agentbox/download-resource-message-v4.html"
const downloadAttachmentMessageBridgeDomain = "https://agentbox.ashray.xyz"
const downloadAttachmentR2CSPDomain = "https://*.r2.cloudflarestorage.com"

// registerDownloadAttachmentMessageBridgeResource adds a tiny MCP Apps view
// for hosts that support ui/message. The canonical tool result remains the
// standard ResourceLink plus structured download_url. On ChatGPT, the view
// fetches the signed R2 URL in the browser, uploads the bytes into ChatGPT's
// native file store, resolves a ChatGPT-owned temporary download URL, and puts
// that URL in a real follow-up turn. This bridges browser network access to
// sandbox downloaders without proxying attachment bytes through AgentBox.
// Generic MCP Apps ResourceLink/text handoffs remain as fallbacks.
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
						"connectDomains":  []string{downloadAttachmentR2CSPDomain},
						"resourceDomains": []string{},
					},
					"domain": downloadAttachmentMessageBridgeDomain,
				},
				"openai/widgetPrefersBorder": true,
				"openai/widgetCSP": map[string]any{
					"connect_domains":  []string{downloadAttachmentR2CSPDomain},
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
  <div class="status" id="status">Preparing attachment handoff…</div>
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

        const nativeFileHandoffAvailable =
          typeof window.openai?.uploadFile === "function" &&
          typeof window.openai?.getFileDownloadUrl === "function" &&
          typeof window.openai?.sendFollowUpMessage === "function";

        if (nativeFileHandoffAvailable) {
          const target = new URL(downloadUrl);
          if (target.protocol !== "https:" || !target.hostname.endsWith(".r2.cloudflarestorage.com")) {
            throw new Error("AgentBox returned an unexpected download origin.");
          }

          const response = await fetch(downloadUrl, { cache: "no-store", credentials: "omit" });
          if (!response.ok) throw new Error("AgentBox file download returned HTTP " + response.status + ".");
          const blob = await response.blob();
          const expectedSize = Number(asset.size_bytes);
          if (Number.isFinite(expectedSize) && expectedSize >= 0 && blob.size !== expectedSize) {
            throw new Error("Downloaded byte count did not match AgentBox metadata.");
          }

          const mimeType = asset.mime_type || blob.type || "application/octet-stream";
          const file = new File([blob], asset.file_name, { type: mimeType });
          const uploaded = await window.openai.uploadFile(file, { library: false });
          if (!uploaded?.fileId) throw new Error("ChatGPT upload returned without a native file handle.");
          const resolved = await window.openai.getFileDownloadUrl({ fileId: uploaded.fileId });
          if (!resolved?.downloadUrl) throw new Error("ChatGPT did not return a native file download URL.");

          const nativeHandoff =
            "AgentBox attachment is available as a ChatGPT-native temporary file download.\n" +
            "file_name: " + asset.file_name + "\n" +
            "mime_type: " + mimeType + "\n" +
            "size_bytes: " + String(blob.size) + "\n" +
            "download_url: " + resolved.downloadUrl + "\n" +
            "Download this exact URL into the sandbox now. The AgentBox bytes were transferred directly from R2 to ChatGPT in the browser; do not call download_attachment again unless this URL expires.";
          await window.openai.sendFollowUpMessage({ prompt: nativeHandoff, scrollToBottom: false });
          statusEl.textContent = asset.file_name + " was uploaded to ChatGPT and handed off for sandbox download.";
          return;
        }

        const initialized = await initializeBridge();
        const messageCapabilities = initialized?.hostCapabilities?.message ?? {};

        const resourceLink = {
          type: "resource_link",
          uri: downloadUrl,
          name: asset.file_name,
          title: asset.file_name,
          mimeType: asset.mime_type || undefined,
          size: asset.size_bytes,
        };

        if (messageCapabilities.resourceLink) {
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
          if (result?.isError) throw new Error("Host rejected the attachment ResourceLink message.");
          statusEl.textContent = asset.file_name + " was added to the conversation as a resource.";
          return;
        }

        const expiry = Number(output?.expires_in) || 300;
        const handoffText =
          "AgentBox attachment ready for sandbox download.\n" +
          "file_name: " + asset.file_name + "\n" +
          "mime_type: " + (asset.mime_type || "application/octet-stream") + "\n" +
          "size_bytes: " + String(asset.size_bytes ?? "") + "\n" +
          "expires_in_seconds: " + String(expiry) + "\n" +
          "download_url: " + downloadUrl + "\n" +
          "Please download this exact URL into the sandbox now. Do not call download_attachment again unless this capability expires.";

        if (messageCapabilities.text) {
          const result = await rpc("ui/message", {
            role: "user",
            content: [{ type: "text", text: handoffText }],
          });
          if (result?.isError) throw new Error("Host rejected the attachment text handoff.");
          statusEl.textContent = asset.file_name + " download capability was sent to the conversation.";
          return;
        }

        throw new Error("This host cannot send the attachment resource or its short-lived download capability into the conversation.");
      }

      run().catch((error) => {
        statusEl.textContent = "Could not add the AgentBox attachment to the conversation: " + safeError(error);
      });
    })();
  </script>
</body>
</html>`
