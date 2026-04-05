package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/security"
)

// MediaSenderFunc sends a file to a channel's native media API.
// connectionID: channel connection ID (e.g. "tg_abc123").
// chatID: platform conversation ID.
// filePath: absolute, sandbox-validated path to the file.
// mimeType: detected MIME type.
// sendAs: "photo", "voice", "video", or "document".
// caption: optional text caption.
type MediaSenderFunc func(ctx context.Context, connectionID, chatID, filePath, mimeType, sendAs, caption string) error

// SessionInfoFunc resolves session context for media delivery.
// Returns platform name, chatID, and connection ID from a session ID.
type SessionInfoFunc func(ctx context.Context, sessionID string) (platform, chatID, connectionID string, err error)

// ConnectionInfoFunc resolves a connection for cross-channel media delivery.
// Returns platform name and the bound agent ID for authorization checks.
type ConnectionInfoFunc func(ctx context.Context, connectionID string) (platform, boundAgentID string, err error)

// Allowed file extensions mapped to MIME types.
var allowedMediaTypes = map[string]string{
	// Images
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	// Audio
	".ogg":  "audio/ogg",
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".m4a":  "audio/mp4",
	".opus": "audio/opus",
	// Video
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".mov":  "video/quicktime",
	// Documents
	".pdf":  "application/pdf",
	".csv":  "text/csv",
	".txt":  "text/plain",
	".json": "application/json",
	".yaml": "application/yaml",
	".yml":  "application/yaml",
	".md":   "text/markdown",
	".html": "text/html",
	".zip":  "application/zip",
}

// Platform file size limits in bytes.
var platformSizeLimits = map[string]int64{
	"telegram": 50 * 1024 * 1024, // 50 MB
	"whatsapp": 16 * 1024 * 1024, // 16 MB
	"discord":  25 * 1024 * 1024, // 25 MB
}

const defaultSizeLimit = 50 * 1024 * 1024 // 50 MB

// RegisterMediaSend registers the send_media tool.
func RegisterMediaSend(reg *Registry, sandbox *security.Sandbox,
	sessionInfo SessionInfoFunc, connInfo ConnectionInfoFunc, senderFn MediaSenderFunc) {

	reg.RegisterWithGroup("send_media",
		"Send a file (image, audio, video, document) to the user via their messaging channel. "+
			"Use after creating or finding a file the user should receive. "+
			"By default sends to the current session's channel. "+
			"To send to a different channel, provide channel and chat_id explicitly.",
		json.RawMessage(`{"type":"object","properties":{`+
			`"path":{"type":"string","description":"File path relative to workspace"},`+
			`"caption":{"type":"string","description":"Optional text caption for the media"},`+
			`"send_as":{"type":"string","enum":["auto","photo","voice","video","document"],`+
			`"description":"How to send: auto detects from file type. Use document to send images as file attachments."},`+
			`"channel":{"type":"string","description":"Connection ID for cross-channel send (e.g. tg_abc123). Omit to use current session."},`+
			`"chat_id":{"type":"string","description":"Chat/user ID on the target channel. Required when channel is specified."}`+
			`},"required":["path"]}`),
		GroupMedia, RiskModerate, "builtin",
		sendMediaTool(sandbox, sessionInfo, connInfo, senderFn))
}

func sendMediaTool(sandbox *security.Sandbox,
	sessionInfo SessionInfoFunc, connInfo ConnectionInfoFunc, senderFn MediaSenderFunc) ToolFunc {

	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			Path    string `json:"path"`
			Caption string `json:"caption"`
			SendAs  string `json:"send_as"`
			Channel string `json:"channel"`
			ChatID  string `json:"chat_id"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		if params.Path == "" {
			return errorResult("path is required"), nil
		}
		if params.SendAs == "" {
			params.SendAs = "auto"
		}

		// 1. Resolve target channel — explicit or from session context.
		var platform, chatID, connectionID string

		if params.Channel != "" {
			// Explicit cross-channel mode: verify agent is bound.
			if params.ChatID == "" {
				return errorResult("chat_id is required when channel is specified"), nil
			}
			agentID := AgentIDFromContext(ctx)
			connPlatform, boundAgent, err := connInfo(ctx, params.Channel)
			if err != nil {
				return errorResult("connection not found: " + err.Error()), nil
			}
			if boundAgent != agentID {
				return errorResult(fmt.Sprintf(
					"agent %q is not bound to connection %q (bound to %q)",
					agentID, params.Channel, boundAgent)), nil
			}
			platform = connPlatform
			chatID = params.ChatID
			connectionID = params.Channel
		} else {
			// Default mode: use current session context.
			sessionID := SessionIDFromCtx(ctx)
			if sessionID == "" {
				return errorResult("send_media requires a channel session or explicit channel/chat_id"), nil
			}
			var err error
			platform, chatID, connectionID, err = sessionInfo(ctx, sessionID)
			if err != nil {
				return errorResult("failed to resolve session: " + err.Error()), nil
			}
		}

		// 2. Validate file path via sandbox.
		resolvedPath, err := sandbox.Resolve(params.Path)
		if err != nil {
			return errorResult("file path outside workspace boundary"), nil
		}

		// 3. Check file exists and get size.
		info, err := os.Stat(resolvedPath)
		if err != nil {
			return errorResult("file not found: " + params.Path), nil
		}
		if info.IsDir() {
			return errorResult("path is a directory, not a file"), nil
		}

		// 4. Check file extension is allowed.
		ext := strings.ToLower(filepath.Ext(resolvedPath))
		mimeType, ok := allowedMediaTypes[ext]
		if !ok {
			return errorResult(fmt.Sprintf("unsupported file type: %s (allowed: images, audio, video, pdf, csv, txt, json, yaml, md, html, zip)", ext)), nil
		}

		// 5. Check file size against platform limit.
		var limit int64 = defaultSizeLimit
		if l, ok := platformSizeLimits[platform]; ok {
			limit = l
		}
		if info.Size() > limit {
			return errorResult(fmt.Sprintf("file too large for %s: %d bytes exceeds %d byte limit",
				platform, info.Size(), limit)), nil
		}

		// 6. Resolve send_as from MIME type if auto.
		sendAs := resolveSendAs(params.SendAs, mimeType)

		// 7. Send via channel adapter.
		if err := senderFn(ctx, connectionID, chatID, resolvedPath, mimeType, sendAs, params.Caption); err != nil {
			return errorResult("failed to send media: " + err.Error()), nil
		}

		filename := filepath.Base(resolvedPath)
		return &canonical.ToolResult{
			Content: fmt.Sprintf("Sent %s as %s to %s", filename, sendAs, platform),
		}, nil
	}
}

// resolveSendAs maps "auto" to a concrete send type based on MIME.
func resolveSendAs(sendAs, mimeType string) string {
	if sendAs != "auto" && sendAs != "" {
		return sendAs
	}
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "photo"
	case strings.HasPrefix(mimeType, "audio/"):
		return "voice"
	case strings.HasPrefix(mimeType, "video/"):
		return "video"
	default:
		return "document"
	}
}

// DetectMediaMIME returns the MIME type for a file extension, or empty if unsupported.
func DetectMediaMIME(ext string) string {
	return allowedMediaTypes[strings.ToLower(ext)]
}
