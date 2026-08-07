package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const downloadAttachmentDiagnosticURI = "ui://agentbox/download-attachment-diagnostic-v4.html"
const downloadAttachmentDiagnosticDomain = "https://agentbox.ashray.xyz"
const downloadAttachmentR2CSPDomain = "https://*.r2.cloudflarestorage.com"

// This widget is intentionally temporary. It inspects ChatGPT's host-normalized
// MCP result for download_attachment so we can determine whether a standard MCP
// ResourceLink receives a native ChatGPT fileId. It never sends signed R2 URLs
// or attachment bytes back to the model.
func registerDownloadAttachmentDiagnosticResource(server *mcp.Server) {
	server.AddResource(&mcp.Resource{
		URI:         downloadAttachmentDiagnosticURI,
		Name:        "agentbox-download-attachment-diagnostic",
		Title:       "AgentBox attachment diagnostic",
		Description: "Temporary diagnostic for ChatGPT tool-returned file references.",
		MIMEType:    "text/html;profile=mcp-app",
	}, func(_ context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI:      downloadAttachmentDiagnosticURI,
			MIMEType: "text/html;profile=mcp-app",
			Text:     downloadAttachmentDiagnosticHTML,
			Meta: mcp.Meta{"ui": map[string]any{
				"prefersBorder": true,
				"csp": map[string]any{
					"connectDomains":  []string{downloadAttachmentR2CSPDomain},
					"resourceDomains": []string{},
				},
				"domain": downloadAttachmentDiagnosticDomain,
			},
				"openai/widgetPrefersBorder": true,
				"openai/widgetCSP": map[string]any{
					"connect_domains":  []string{downloadAttachmentR2CSPDomain},
					"resource_domains": []string{},
				},
				"openai/widgetDomain": downloadAttachmentDiagnosticDomain,
			},
		}}}, nil
	})
}

const downloadAttachmentDiagnosticHTML = `<!doctype html>
<html>
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width,initial-scale=1" />
  <title>AgentBox attachment diagnostic</title>
  <style>
    :root { color-scheme: light dark; font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
    body { margin: 0; padding: 12px; }
    .title { font-family: system-ui, sans-serif; font-weight: 650; margin-bottom: 8px; }
    .status { font-family: system-ui, sans-serif; font-size: 13px; opacity: .75; margin-bottom: 8px; }
    pre { white-space: pre-wrap; overflow-wrap: anywhere; font-size: 11px; line-height: 1.4; margin: 0; }
  </style>
</head>
<body>
  <div class="title">AgentBox download diagnostic</div>
  <div class="status" id="status">Inspecting ChatGPT file reference…</div>
  <pre id="out"></pre>
  <script>
    (() => {
      const statusEl = document.getElementById("status");
      const outEl = document.getElementById("out");
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

      function collectShape(value, path = "$", output = [], depth = 0) {
        if (output.length >= 120 || depth > 8) return output;
        if (Array.isArray(value)) {
          output.push(path + ":array[" + value.length + "]");
          for (let i = 0; i < Math.min(value.length, 8); i++) collectShape(value[i], path + "[" + i + "]", output, depth + 1);
          return output;
        }
        if (value && typeof value === "object") {
          const keys = Object.keys(value);
          output.push(path + ":object{" + keys.join(",") + "}");
          for (const key of keys.slice(0, 20)) collectShape(value[key], path + "." + key, output, depth + 1);
          return output;
        }
        output.push(path + ":" + (value === null ? "null" : typeof value));
        return output;
      }

      function collectFileIds(value, path = "$", found = [], seen = new Set(), depth = 0) {
        if (depth > 10 || found.length >= 20) return found;
        if (Array.isArray(value)) {
          value.forEach((item, index) => collectFileIds(item, path + "[" + index + "]", found, seen, depth + 1));
          return found;
        }
        if (!value || typeof value !== "object") return found;
        for (const [key, item] of Object.entries(value)) {
          const itemPath = path + "." + key;
          if (typeof item === "string") {
            const normalized = key.toLowerCase().replace(/[^a-z0-9]/g, "");
            const looksLikeFileId = /^file_[A-Za-z0-9_-]+$/.test(item) || normalized === "fileid";
            if (looksLikeFileId && !seen.has(item)) {
              seen.add(item);
              found.push({ path: itemPath, fileId: item });
            }
          } else {
            collectFileIds(item, itemPath, found, seen, depth + 1);
          }
        }
        return found;
      }

      async function run() {
        const openai = window.openai;
        const metadata = await waitForOpenAIGlobal("toolResponseMetadata");

        const candidates = collectFileIds(metadata);
        const checks = [];
        for (const candidate of candidates.slice(0, 8)) {
          if (!openai?.getFileDownloadUrl) {
            checks.push({ fileId: candidate.fileId, path: candidate.path, getFileDownloadUrl: "unavailable" });
            continue;
          }
          try {
            const resolved = await openai.getFileDownloadUrl({ fileId: candidate.fileId });
            checks.push({
              fileId: candidate.fileId,
              path: candidate.path,
              getFileDownloadUrl: resolved?.downloadUrl ? "success" : "returned_without_download_url",
            });
          } catch (error) {
            checks.push({
              fileId: candidate.fileId,
              path: candidate.path,
              getFileDownloadUrl: "error",
              error: String(error?.message ?? error).slice(0, 240),
            });
          }
        }

        const callResult = metadata?.call_tool_result;
        const resourceLink = Array.isArray(callResult?.content)
          ? callResult.content.find((item) => item?.type === "resource_link" && typeof item?.uri === "string")
          : null;
        const uploadProbe = {
          resourceLinkFound: Boolean(resourceLink),
          widgetOrigin: window.location.origin,
          resourceIsR2: false,
          corsFetch: "not_attempted",
          noCorsFetch: "not_attempted",
          fetch: "not_attempted",
          fetchedBytes: 0,
          uploadFile: "not_attempted",
          uploadedFileId: null,
          uploadedFileDownloadUrl: "not_attempted",
        };

        if (resourceLink && typeof openai?.uploadFile === "function") {
          try {
            const target = new URL(resourceLink.uri);
            uploadProbe.resourceIsR2 = target.hostname.endsWith(".r2.cloudflarestorage.com");
          } catch (_) {}

          try {
            const response = await fetch(resourceLink.uri, {
              cache: "no-store",
              credentials: "omit",
              mode: "no-cors",
            });
            uploadProbe.noCorsFetch = "success:" + response.type;
          } catch (error) {
            uploadProbe.noCorsFetch = "error";
            uploadProbe.noCorsError = String(error?.message ?? error).slice(0, 240);
          }

          try {
            const response = await fetch(resourceLink.uri, { cache: "no-store", credentials: "omit" });
            if (!response.ok) throw new Error("R2 fetch returned HTTP " + response.status);
            uploadProbe.corsFetch = "success";
            const blob = await response.blob();
            uploadProbe.fetch = "success";
            uploadProbe.fetchedBytes = blob.size;
            const fileName = resourceLink.name || resourceLink.title || "agentbox-attachment.bin";
            const mimeType = resourceLink.mimeType || blob.type || "application/octet-stream";
            const file = new File([blob], fileName, { type: mimeType });
            const uploaded = await openai.uploadFile(file, { library: false });
            if (!uploaded?.fileId) throw new Error("uploadFile returned without fileId");
            uploadProbe.uploadFile = "success";
            uploadProbe.uploadedFileId = uploaded.fileId;
            if (typeof openai?.getFileDownloadUrl === "function") {
              const resolved = await openai.getFileDownloadUrl({ fileId: uploaded.fileId });
              uploadProbe.uploadedFileDownloadUrl = resolved?.downloadUrl ? "success" : "returned_without_download_url";
            } else {
              uploadProbe.uploadedFileDownloadUrl = "unavailable";
            }
          } catch (error) {
            if (uploadProbe.corsFetch === "not_attempted") uploadProbe.corsFetch = "error";
            if (uploadProbe.fetch === "not_attempted") uploadProbe.fetch = "error";
            if (uploadProbe.fetch === "success" && uploadProbe.uploadFile === "not_attempted") uploadProbe.uploadFile = "error";
            uploadProbe.error = String(error?.message ?? error).slice(0, 300);
          }
        } else if (resourceLink) {
          uploadProbe.uploadFile = "unavailable";
        }

        const summary = {
          diagnostic: "agentbox-download-attachment-v4",
          hasWindowOpenAI: Boolean(openai),
          hasToolResponseMetadata: Boolean(metadata),
          metadataTopLevelKeys: metadata && typeof metadata === "object" ? Object.keys(metadata) : [],
          hasCallToolResult: Boolean(metadata?.call_tool_result),
          hasMcpToolResult: Boolean(metadata?.mcp_tool_result),
          getFileDownloadUrlAvailable: typeof openai?.getFileDownloadUrl === "function",
          uploadFileAvailable: typeof openai?.uploadFile === "function",
          fileIdCandidates: candidates,
          downloadUrlChecks: checks,
          uploadProbe,
          metadataShape: collectShape(metadata),
        };

        statusEl.textContent = uploadProbe.uploadFile === "success"
          ? "Diagnostic complete — R2 file uploaded to ChatGPT natively."
          : (candidates.length ? "Diagnostic complete — native file candidate found." : "Diagnostic complete — no native fileId found.");
        outEl.textContent = JSON.stringify(summary, null, 2);

        // Send only the sanitized structural diagnostic to the conversation.
        // No signed R2 URI or file bytes are included.
        if (openai?.sendFollowUpMessage && !window.__agentboxDownloadDiagnosticSent) {
          window.__agentboxDownloadDiagnosticSent = true;
          try {
            await openai.sendFollowUpMessage({
              prompt: "AgentBox download diagnostic result (sanitized; no signed URLs or file bytes):\n" + JSON.stringify(summary, null, 2),
              scrollToBottom: false,
            });
          } catch (error) {
            statusEl.textContent += " Could not post the diagnostic follow-up: " + String(error?.message ?? error).slice(0, 160);
          }
        }
      }

      run().catch((error) => {
        statusEl.textContent = "Diagnostic failed.";
        outEl.textContent = String(error?.stack ?? error);
      });
    })();
  </script>
</body>
</html>`
