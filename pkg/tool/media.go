package tool

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
	"github.com/xoai/sageclaw/pkg/security"
)

// imageExts lists supported image extensions.
var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
}

// textReadableExts lists file types that can be read as text directly.
var textReadableExts = map[string]string{
	".json": "application/json", ".csv": "text/csv", ".html": "text/html",
	".htm": "text/html", ".xml": "application/xml", ".md": "text/markdown",
	".txt": "text/plain", ".yaml": "application/yaml", ".yml": "application/yaml",
	".toml": "application/toml", ".ini": "text/plain", ".cfg": "text/plain",
	".log": "text/plain", ".env": "text/plain",
}

const (
	maxTextDocBytes = 500_000    // 500KB for direct text return
	maxImageURLBytes = 20_000_000 // 20MB max for URL-fetched images
)

// RegisterMedia registers image, document, and TTS tools.
// providers is a list of providers to try for vision/document/image-gen/TTS tasks.
func RegisterMedia(reg *Registry, sandbox *security.Sandbox, providers []provider.Provider, configReader ...ConfigReader) {
	var cr ConfigReader
	if len(configReader) > 0 {
		cr = configReader[0]
	}

	reg.RegisterWithGroup("read_image", "Analyze an image using a vision-capable AI provider",
		json.RawMessage(`{"type":"object","properties":{`+
			`"path":{"type":"string","description":"Image file path or HTTP/HTTPS URL"},`+
			`"question":{"type":"string","description":"Custom question about the image (default: describe in detail)"}`+
			`},"required":["path"]}`),
		GroupMedia, RiskModerate, "builtin", readImage(sandbox, providers))

	reg.RegisterWithGroup("read_document", "Read and analyze a document (text files returned directly, binary docs analyzed by AI)",
		json.RawMessage(`{"type":"object","properties":{`+
			`"path":{"type":"string","description":"Document file path relative to workspace"}`+
			`},"required":["path"]}`),
		GroupMedia, RiskModerate, "builtin", readDocument(sandbox, providers))

	reg.RegisterWithGroup("create_image", "Generate an image from a text description",
		json.RawMessage(`{"type":"object","properties":{`+
			`"prompt":{"type":"string","description":"Image description"},`+
			`"size":{"type":"string","description":"Image size: 1024x1024, 1024x1792, or 1792x1024"}`+
			`},"required":["prompt"]}`),
		GroupMedia, RiskSensitive, "builtin", createImage(sandbox, providers, cr))

	reg.RegisterWithGroup("text_to_speech", "Convert text to speech audio",
		json.RawMessage(`{"type":"object","properties":{`+
			`"text":{"type":"string","description":"Text to convert to speech"},`+
			`"voice":{"type":"string","description":"Voice preset (provider-specific, optional)"}`+
			`},"required":["text"]}`),
		GroupMedia, RiskModerate, "builtin", textToSpeech(sandbox, providers, cr))

	// Config schemas.
	reg.SetConfigSchema("text_to_speech", map[string]ToolConfigField{
		"tts_model": {
			Type:        "select",
			Description: "TTS model to use",
			Default:     "gemini-2.5-flash-preview-tts",
			Options:     []string{"gemini-2.5-flash-preview-tts"},
		},
		"default_voice": {
			Type:        "string",
			Description: "Default voice preset (e.g. Kore, Puck, Charon, Fenrir, Aoede)",
		},
	})
	reg.SetConfigSchema("create_image", map[string]ToolConfigField{
		"default_size": {
			Type:        "select",
			Description: "Default image size",
			Default:     "1024x1024",
			Options:     []string{"1024x1024", "1024x1792", "1792x1024"},
		},
	})
}

// tryProviders iterates capable providers and returns the first successful result.
// Logs failures for debugging; returns the last error if all fail.
func tryProviders(ctx context.Context, providers []provider.Provider, cap string, fn func(p provider.Provider) (*canonical.ToolResult, error)) (*canonical.ToolResult, error) {
	var lastErr error
	tried := 0
	for _, p := range providers {
		if !provider.ProviderSupports(p, cap) {
			continue
		}
		tried++
		result, err := fn(p)
		if err == nil && result != nil && !result.IsError {
			return result, nil
		}
		if err != nil {
			lastErr = err
			log.Printf("media: provider %s failed for %s: %v", p.Name(), cap, err)
		} else if result != nil && result.IsError {
			lastErr = fmt.Errorf("%s", result.Content)
			log.Printf("media: provider %s returned error for %s: %s", p.Name(), cap, result.Content)
		}
	}
	if tried == 0 {
		return errorResult(fmt.Sprintf("no %s-capable provider configured", cap)), nil
	}
	if lastErr != nil {
		return errorResult(fmt.Sprintf("%s failed (tried %d providers): %v", cap, tried, lastErr)), nil
	}
	return errorResult(fmt.Sprintf("%s failed: all %d providers returned errors", cap, tried)), nil
}

func readImage(sandbox *security.Sandbox, providers []provider.Provider) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			Path     string `json:"path"`
			Question string `json:"question"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		// Determine prompt.
		question := "Describe this image in detail. What do you see?"
		if params.Question != "" {
			question = params.Question
		}

		var data []byte
		var mimeType string

		if isHTTPURL(params.Path) {
			// URL path: fetch with SSRF check.
			fetched, mime, err := fetchImageURL(params.Path)
			if err != nil {
				return errorResult(err.Error()), nil
			}
			data = fetched
			mimeType = mime
		} else {
			// File path.
			ext := strings.ToLower(filepath.Ext(params.Path))
			if !imageExts[ext] {
				return errorResult(fmt.Sprintf("unsupported image format %q — supported: png, jpg, jpeg, gif, webp", ext)), nil
			}

			resolved, err := sandbox.Resolve(params.Path)
			if err != nil {
				return errorResult("access denied: " + err.Error()), nil
			}

			data, err = os.ReadFile(resolved)
			if err != nil {
				return errorResult("read failed: " + err.Error()), nil
			}

			mimeType = "image/" + strings.TrimPrefix(ext, ".")
			if ext == ".jpg" {
				mimeType = "image/jpeg"
			}
		}

		b64 := base64.StdEncoding.EncodeToString(data)

		return tryProviders(ctx, providers, provider.CapVision, func(p provider.Provider) (*canonical.ToolResult, error) {
			req := &canonical.Request{
				Messages: []canonical.Message{
					{
						Role: "user",
						Content: []canonical.Content{
							{Type: "image", Source: &canonical.ImageSource{
								Type:      "base64",
								MediaType: mimeType,
								Data:      b64,
							}},
							{Type: "text", Text: question},
						},
					},
				},
				MaxTokens: 1024,
			}
			resp, err := p.Chat(ctx, req)
			if err != nil {
				return nil, err
			}
			return &canonical.ToolResult{Content: extractTextFromResponse(resp)}, nil
		})
	}
}

func readDocument(sandbox *security.Sandbox, providers []provider.Provider) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		ext := strings.ToLower(filepath.Ext(params.Path))

		// Fast path: text-readable files returned directly.
		if _, ok := textReadableExts[ext]; ok {
			resolved, err := sandbox.Resolve(params.Path)
			if err != nil {
				return errorResult("access denied: " + err.Error()), nil
			}
			data, err := os.ReadFile(resolved)
			if err != nil {
				return errorResult("read failed: " + err.Error()), nil
			}
			content := string(data)
			if len(content) > maxTextDocBytes {
				content = content[:maxTextDocBytes] + "\n... [truncated at 500KB]"
			}
			return &canonical.ToolResult{Content: content}, nil
		}

		// Binary documents (PDF, DOCX, etc.) — try providers with fallback.
		resolved, err := sandbox.Resolve(params.Path)
		if err != nil {
			return errorResult("access denied: " + err.Error()), nil
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			return errorResult("read failed: " + err.Error()), nil
		}

		b64 := base64.StdEncoding.EncodeToString(data)
		mime := mimeForExt(ext)

		return tryProviders(ctx, providers, provider.CapDocument, func(p provider.Provider) (*canonical.ToolResult, error) {
			req := &canonical.Request{
				Messages: []canonical.Message{
					{
						Role: "user",
						Content: []canonical.Content{
							{Type: "document", Source: &canonical.ImageSource{
								Type:      "base64",
								MediaType: mime,
								Data:      b64,
							}},
							{Type: "text", Text: "Analyze this document. Provide a summary of its contents."},
						},
					},
				},
				MaxTokens: 2048,
			}
			resp, err := p.Chat(ctx, req)
			if err != nil {
				return nil, err
			}
			return &canonical.ToolResult{Content: extractTextFromResponse(resp)}, nil
		})
	}
}

func createImage(sandbox *security.Sandbox, providers []provider.Provider, cr ConfigReader) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			Prompt string `json:"prompt"`
			Size   string `json:"size"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		if params.Size == "" {
			if cr != nil {
				params.Size = cr(ctx, "create_image", "default_size")
			}
			if params.Size == "" {
				params.Size = "1024x1024"
			}
		}

		return tryProviders(ctx, providers, provider.CapImageGen, func(p provider.Provider) (*canonical.ToolResult, error) {
			req := &canonical.Request{
				Messages: []canonical.Message{
					{
						Role: "user",
						Content: []canonical.Content{
							{Type: "text", Text: fmt.Sprintf("Generate an image: %s (size: %s)", params.Prompt, params.Size)},
						},
					},
				},
				MaxTokens: 1024,
			}
			resp, err := p.Chat(ctx, req)
			if err != nil {
				return nil, err
			}

			// Look for image content in response.
			for _, msg := range resp.Messages {
				for _, c := range msg.Content {
					if c.Type == "image" && c.Source != nil && c.Source.Data != "" {
						imgData, err := base64.StdEncoding.DecodeString(c.Source.Data)
						if err != nil {
							return errorResult("decode image: " + err.Error()), nil
						}

						dir := filepath.Join(sandbox.Root(), "generated")
						if err := os.MkdirAll(dir, 0755); err != nil {
						return errorResult("create output dir: " + err.Error()), nil
					}
						name := fmt.Sprintf("generated_%d.png", time.Now().UnixMilli())
						path := filepath.Join(dir, name)
						if err := os.WriteFile(path, imgData, 0644); err != nil {
							return errorResult("save image: " + err.Error()), nil
						}
						return &canonical.ToolResult{Content: fmt.Sprintf("Image generated and saved: %s", path)}, nil
					}
				}
			}

			text := extractTextFromResponse(resp)
			return &canonical.ToolResult{Content: "Image generation returned text: " + text}, nil
		})
	}
}

func textToSpeech(sandbox *security.Sandbox, providers []provider.Provider, cr ConfigReader) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			Text  string `json:"text"`
			Voice string `json:"voice"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		if strings.TrimSpace(params.Text) == "" {
			return errorResult("text is required"), nil
		}

		// Resolve config: ConfigReader -> hardcoded defaults.
		ttsModel := "gemini-2.5-flash-preview-tts"
		if cr != nil {
			if m := cr(ctx, "text_to_speech", "tts_model"); m != "" {
				ttsModel = m
			}
			if params.Voice == "" {
				params.Voice = cr(ctx, "text_to_speech", "default_voice")
			}
		}

		return tryProviders(ctx, providers, provider.CapTTS, func(p provider.Provider) (*canonical.ToolResult, error) {
			req := &canonical.Request{
				Model: ttsModel,
				Messages: []canonical.Message{
					{
						Role: "user",
						Content: []canonical.Content{
							{Type: "text", Text: params.Text},
						},
					},
				},
				MaxTokens: 8192,
				Options: map[string]any{
					"response_modalities": []string{"AUDIO"},
				},
			}
			if params.Voice != "" {
				req.Options["speech_voice"] = params.Voice
			}
			resp, err := p.Chat(ctx, req)
			if err != nil {
				return nil, err
			}

			// Look for audio content in response.
			// Gemini returns TTS audio via Content.Source (ImageSource with base64).
			// Future providers may use Content.Audio (AudioSource with FilePath).
			for _, msg := range resp.Messages {
				for _, c := range msg.Content {
					if c.Type != "audio" {
						continue
					}

					// Path 1: inline base64 audio (Gemini TTS).
					if c.Source != nil && c.Source.Data != "" {
						audioData, err := base64.StdEncoding.DecodeString(c.Source.Data)
						if err != nil {
							return errorResult("decode audio: " + err.Error()), nil
						}

						dir := filepath.Join(sandbox.Root(), "generated")
						if err := os.MkdirAll(dir, 0755); err != nil {
							return errorResult("create output dir: " + err.Error()), nil
						}

						// Detect format from MIME type and convert if needed.
						ext := ".wav"
						saveData := audioData
						mime := c.Source.MediaType
						if strings.Contains(mime, "L16") || strings.Contains(mime, "pcm") {
							// Raw PCM (e.g. audio/L16;codec=pcm;rate=24000) — wrap in WAV header.
							sampleRate := 24000 // Gemini TTS default
							saveData = pcmToWAV(audioData, sampleRate, 1, 16)
						} else if strings.Contains(mime, "ogg") {
							ext = ".ogg"
						} else if strings.Contains(mime, "mp3") || strings.Contains(mime, "mpeg") {
							ext = ".mp3"
						}

						name := fmt.Sprintf("tts_%d%s", time.Now().UnixMilli(), ext)
						path := filepath.Join(dir, name)
						if err := os.WriteFile(path, saveData, 0644); err != nil {
							return errorResult("save audio: " + err.Error()), nil
						}
						return &canonical.ToolResult{Content: fmt.Sprintf("Audio generated and saved: %s", path)}, nil
					}

					// Path 2: file-path audio (AudioSource, e.g. from future providers).
					if c.Audio != nil && c.Audio.FilePath != "" {
						return &canonical.ToolResult{Content: fmt.Sprintf("Audio generated and saved: %s", c.Audio.FilePath)}, nil
					}
				}
			}

			return errorResult("TTS provider did not return audio content"), nil
		})
	}
}

// pcmToWAV wraps raw PCM audio bytes in a WAV (RIFF) header.
// Returns a valid WAV file that can be played by any audio player.
func pcmToWAV(pcmData []byte, sampleRate, numChannels, bitsPerSample int) []byte {
	dataSize := len(pcmData)
	byteRate := sampleRate * numChannels * bitsPerSample / 8
	blockAlign := numChannels * bitsPerSample / 8

	buf := &bytes.Buffer{}
	buf.Grow(44 + dataSize)

	// RIFF header.
	buf.WriteString("RIFF")
	binary.Write(buf, binary.LittleEndian, uint32(36+dataSize)) // file size - 8
	buf.WriteString("WAVE")

	// fmt sub-chunk.
	buf.WriteString("fmt ")
	binary.Write(buf, binary.LittleEndian, uint32(16))                // sub-chunk size
	binary.Write(buf, binary.LittleEndian, uint16(1))                 // PCM format
	binary.Write(buf, binary.LittleEndian, uint16(numChannels))       // channels
	binary.Write(buf, binary.LittleEndian, uint32(sampleRate))        // sample rate
	binary.Write(buf, binary.LittleEndian, uint32(byteRate))          // byte rate
	binary.Write(buf, binary.LittleEndian, uint16(blockAlign))        // block align
	binary.Write(buf, binary.LittleEndian, uint16(bitsPerSample))     // bits per sample

	// data sub-chunk.
	buf.WriteString("data")
	binary.Write(buf, binary.LittleEndian, uint32(dataSize))
	buf.Write(pcmData)

	return buf.Bytes()
}

// isHTTPURL returns true if the path looks like an HTTP/HTTPS URL.
func isHTTPURL(path string) bool {
	return strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")
}

// fetchImageURL downloads an image from a URL with SSRF check and content validation.
func fetchImageURL(urlStr string) ([]byte, string, error) {
	if err := checkSSRF(urlStr); err != nil {
		return nil, "", err
	}

	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{DialContext: pinnedDialer()},
	}

	resp, err := client.Get(urlStr)
	if err != nil {
		return nil, "", fmt.Errorf("fetch image URL failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("fetch image URL returned status %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", fmt.Errorf("URL content type %q is not an image", contentType)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageURLBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("reading image URL: %w", err)
	}
	if len(data) > maxImageURLBytes {
		return nil, "", fmt.Errorf("image too large (max %dMB)", maxImageURLBytes/1_000_000)
	}

	return data, contentType, nil
}

// extractTextFromResponse collects text content from response messages.
func extractTextFromResponse(resp *canonical.Response) string {
	var sb strings.Builder
	for _, msg := range resp.Messages {
		for _, c := range msg.Content {
			if c.Type == "text" {
				sb.WriteString(c.Text)
			}
		}
	}
	return sb.String()
}

func mimeForExt(ext string) string {
	switch ext {
	case ".pdf":
		return "application/pdf"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".doc":
		return "application/msword"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	default:
		return "application/octet-stream"
	}
}
