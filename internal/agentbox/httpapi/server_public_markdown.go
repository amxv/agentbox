package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"agentbox/internal/agentbox/messageformat"
	"agentbox/internal/agentbox/service"
	"agentbox/internal/agentbox/types"
)

func (s *Server) publicThreadMarkdown(w http.ResponseWriter, r *http.Request, token string) {
	thread, err := s.service.GetPublicThread(r.Context(), token)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	markdown, err := s.renderPublicThreadMarkdown(r, token, thread)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `inline; filename="agentbox-thread.md"`)
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(markdown))
}

func (s *Server) renderPublicThreadMarkdown(r *http.Request, token string, thread *types.PublicThreadView) (string, error) {
	var builder strings.Builder
	title := strings.TrimSpace(thread.Title)
	if title == "" {
		title = "Untitled thread"
	}

	fmt.Fprintf(&builder, "# %s\n\n", markdownInline(title))
	fmt.Fprintf(&builder, "- Thread: `%s`\n", thread.ID)
	fmt.Fprintf(&builder, "- Created by: %s\n", markdownInline(publicAttributionLabel(thread.CreatedByUserDisplayName, thread.CreatedByActorName, thread.CreatedBy)))
	fmt.Fprintf(&builder, "- Created: %s\n", thread.CreatedAt)
	fmt.Fprintf(&builder, "- Updated: %s\n", thread.UpdatedAt)
	fmt.Fprintf(&builder, "- Messages: %d\n", len(thread.Messages))
	if baseURL := s.requestBaseURL(r); baseURL != "" {
		fmt.Fprintf(&builder, "- Web: <%s/share/%s>\n", strings.TrimRight(baseURL, "/"), token)
		fmt.Fprintf(&builder, "- Markdown: <%s/share/%s.md>\n", strings.TrimRight(baseURL, "/"), token)
	}
	builder.WriteString("\n> Attachment links are short-lived presigned Cloudflare R2 URLs. Refresh this Markdown URL if a link expires.\n")

	for index, message := range thread.Messages {
		fmt.Fprintf(&builder, "\n---\n\n## Message %d\n\n", index+1)
		fmt.Fprintf(&builder, "- ID: `%s`\n", message.ID)
		fmt.Fprintf(&builder, "- Author: %s\n", markdownInline(publicAttributionLabel(message.CreatedByUserDisplayName, message.CreatedByActorName, message.Author)))
		fmt.Fprintf(&builder, "- Created: %s\n", message.CreatedAt)
		contentType := publicMessageContentType(message)
		fmt.Fprintf(&builder, "- Content type: `%s`\n", contentType)
		fmt.Fprintf(&builder, "- Characters: %d\n", utf8.RuneCountInString(message.Body))

		if strings.TrimSpace(message.Body) != "" {
			builder.WriteString("\n")
			if contentType == messageformat.Markdown {
				builder.WriteString(message.Body)
				if !strings.HasSuffix(message.Body, "\n") {
					builder.WriteString("\n")
				}
			} else {
				writePlainTextFence(&builder, message.Body)
			}
		}

		if len(message.Assets) == 0 {
			continue
		}
		builder.WriteString("\n### Attachments\n\n")
		for _, asset := range message.Assets {
			if asset.PurgedAt != nil {
				fmt.Fprintf(&builder, "- %s — deleted by deployment owner\n", markdownInline(asset.FileName))
				continue
			}
			downloadURL, err := s.service.PublicAssetDownloadURL(r.Context(), token, asset.ID)
			if err != nil {
				var coded service.CodedError
				if errors.As(err, &coded) && (coded.Code == "ATTACHMENT_UNAVAILABLE" || coded.Code == "ATTACHMENT_PURGED" || coded.Code == "PUBLIC_ASSET_NOT_FOUND") {
					fmt.Fprintf(&builder, "- %s — unavailable: %s\n", markdownInline(asset.FileName), markdownInline(coded.Message))
					continue
				}
				return "", err
			}
			fmt.Fprintf(&builder, "- [%s](<%s>) — `%s`, %d bytes, asset `%s`\n", markdownLinkText(asset.FileName), downloadURL, publicAssetContentType(asset), asset.SizeBytes, asset.ID)
		}
	}

	return builder.String(), nil
}

func publicAttributionLabel(userDisplayName *string, actorName *string, fallback string) string {
	user := strings.TrimSpace(optionalPublicString(userDisplayName))
	actor := strings.TrimSpace(optionalPublicString(actorName))
	if user != "" && actor != "" && !strings.EqualFold(user, actor) {
		return user + " · " + actor
	}
	if user != "" {
		return user
	}
	if actor != "" {
		return actor
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	return "Agentbox user"
}

func publicMessageContentType(message types.PublicMessage) string {
	if message.BodyContentType == nil || strings.TrimSpace(*message.BodyContentType) == "" {
		return messageformat.Plain
	}
	return strings.TrimSpace(*message.BodyContentType)
}

func publicAssetContentType(asset types.PublicAsset) string {
	if asset.MimeType == nil || strings.TrimSpace(*asset.MimeType) == "" {
		return "application/octet-stream"
	}
	return strings.TrimSpace(*asset.MimeType)
}

func optionalPublicString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func markdownInline(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"`", "\\`",
		"*", "\\*",
		"_", "\\_",
		"[", "\\[",
		"]", "\\]",
		"<", "\\<",
		">", "\\>",
	)
	return replacer.Replace(value)
}

func markdownLinkText(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "[", "\\[")
	value = strings.ReplaceAll(value, "]", "\\]")
	return value
}

func writePlainTextFence(builder *strings.Builder, body string) {
	fenceLength := 3
	run := 0
	for _, char := range body {
		if char == '`' {
			run++
			if run >= fenceLength {
				fenceLength = run + 1
			}
			continue
		}
		run = 0
	}
	fence := strings.Repeat("`", fenceLength)
	fmt.Fprintf(builder, "%stext\n%s", fence, body)
	if !strings.HasSuffix(body, "\n") {
		builder.WriteString("\n")
	}
	fmt.Fprintf(builder, "%s\n", fence)
}
