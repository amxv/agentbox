package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const downloadAttachmentBridgeURI = "ui://agentbox/download-attachment-v1.html"
const downloadAttachmentBridgeDomain = "https://agentbox.ashray.xyz"
const downloadAttachmentR2CSPDomain = "https://*.r2.cloudflarestorage.com"

// registerDownloadAttachmentBridgeResource adds the ChatGPT-specific bridge
// used by download_attachment. Generic MCP hosts can consume the standard
// ResourceLink directly. In ChatGPT, this widget fetches the same signed R2 URL
// in the browser, saves the bytes into ChatGPT Library, and posts a model-visible
// handoff telling the agent how to locate and materialize the file.
func registerDownloadAttachmentBridgeResource(server *mcp.Server) {
	server.AddResource(&mcp.Resource{
		URI:         downloadAttachmentBridgeURI,
		Name:        "agentbox-download-attachment",
		Title:       "AgentBox attachment transfer",
		Description: "Transfers one AgentBox attachment directly from R2 into ChatGPT Library.",
		MIMEType:    "text/html;profile=mcp-app",
	}, func(_ context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI:      downloadAttachmentBridgeURI,
			MIMEType: "text/html;profile=mcp-app",
			Text:     downloadAttachmentBridgeHTML,
			Meta: mcp.Meta{"ui": map[string]any{
				"prefersBorder": true,
				"csp": map[string]any{
					"connectDomains":  []string{downloadAttachmentR2CSPDomain},
					"resourceDomains": []string{},
				},
				"domain": downloadAttachmentBridgeDomain,
			},
				"openai/widgetPrefersBorder": true,
				"openai/widgetCSP": map[string]any{
					"connect_domains":  []string{downloadAttachmentR2CSPDomain},
					"resource_domains": []string{},
				},
				"openai/widgetDomain": downloadAttachmentBridgeDomain,
			},
		}}}, nil
	})
}

const downloadAttachmentBridgeHTML = `<!doctype html>
<html>
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width,initial-scale=1" />
  <title>AgentBox attachment transfer</title>
  <style>
    :root { color-scheme: light dark; font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    body { margin: 0; padding: 12px; }
    .title { font-weight: 650; margin-bottom: 6px; }
    .status { font-size: 13px; opacity: .78; }
    .error { white-space: pre-wrap; overflow-wrap: anywhere; }
  </style>
</head>
<body>
  <div class="title">AgentBox attachment</div>
  <div class="status" id="status">Saving file to ChatGPT Library…</div>
  <script>
    (() => {
      const statusEl = document.getElementById("status");
      const OPENAI_SET_GLOBALS_EVENT = "openai:set_globals";

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

      async function postHandoff(openai, prompt) {
        if (!openai?.sendFollowUpMessage || window.__agentboxAttachmentHandoffSent) return false;
        window.__agentboxAttachmentHandoffSent = true;
        await openai.sendFollowUpMessage({ prompt, scrollToBottom: false });
        return true;
      }

      async function run() {
        const openai = window.openai;
        if (!openai?.uploadFile) throw new Error("ChatGPT file upload API is unavailable.");

        const metadata = await waitForOpenAIGlobal("toolResponseMetadata");
        const callResult = metadata?.call_tool_result;
        const resourceLink = Array.isArray(callResult?.content)
          ? callResult.content.find((item) => item?.type === "resource_link" && typeof item?.uri === "string")
          : null;
        const asset = callResult?.structuredContent?.asset ?? {};

        if (!resourceLink) throw new Error("AgentBox did not provide a downloadable file link.");

        const target = new URL(resourceLink.uri);
        if (target.protocol !== "https:" || !target.hostname.endsWith(".r2.cloudflarestorage.com")) {
          throw new Error("AgentBox returned an unexpected download origin.");
        }

        const response = await fetch(resourceLink.uri, { cache: "no-store", credentials: "omit" });
        if (!response.ok) throw new Error("AgentBox file download returned HTTP " + response.status + ".");
        const blob = await response.blob();

        const expectedSize = Number(resourceLink.size ?? asset.size_bytes);
        if (Number.isFinite(expectedSize) && expectedSize >= 0 && blob.size !== expectedSize) {
          throw new Error("Downloaded byte count did not match AgentBox metadata.");
        }

        const fileName = resourceLink.name || resourceLink.title || asset.file_name || "agentbox-attachment.bin";
        const mimeType = resourceLink.mimeType || asset.mime_type || blob.type || "application/octet-stream";
        const file = new File([blob], fileName, { type: mimeType });
        const uploaded = await openai.uploadFile(file, { library: true });
        if (!uploaded?.fileId) throw new Error("ChatGPT saved the file without returning a file handle.");

		// Best-effort validation that ChatGPT recognizes the returned native file
		// handle. A transient validation failure must not turn a successful
		// Library save into a false transfer failure. The signed ChatGPT URL is
		// intentionally never exposed to the model or rendered by this widget.
		if (typeof openai.getFileDownloadUrl === "function") {
		  try { await openai.getFileDownloadUrl({ fileId: uploaded.fileId }); } catch (_) {}
		}

        statusEl.textContent = fileName + " is ready in ChatGPT Library.";

        const handoff = {
          source: "agentbox",
          action: "attachment_saved_to_chatgpt_library",
          asset_id: asset.id ?? null,
          file_name: fileName,
          mime_type: mimeType,
          size_bytes: blob.size,
          saved_at: new Date().toISOString(),
        };
        const prompt =
          "AgentBox attachment transfer completed. The file is now saved in ChatGPT Library.\n" +
          JSON.stringify(handoff, null, 2) +
          "\nUse the Files tool with surface=library to locate this exact file_name (prefer the newest exact-name match if duplicates exist), then materialize it as raw_file into the sandbox when programmatic access is needed. Do not call download_attachment again unless the file cannot be found.";

        try {
          await postHandoff(openai, prompt);
        } catch (error) {
          statusEl.textContent += " The file was saved, but the agent handoff message could not be posted.";
        }
      }

      run().catch(async (error) => {
        const message = safeError(error);
        statusEl.classList.add("error");
        statusEl.textContent = "Could not save the AgentBox attachment to ChatGPT Library: " + message;

        try {
          await postHandoff(window.openai, "AgentBox attachment transfer failed before the file reached ChatGPT Library. Error: " + message + " Retry download_attachment for the same asset if needed.");
        } catch (_) {}
      });
    })();
  </script>
</body>
</html>`
