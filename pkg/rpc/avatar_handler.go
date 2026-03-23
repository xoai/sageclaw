package rpc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// handleAvatarGenerate generates an avatar for an agent using Gemini Imagen.
func (s *Server) handleAvatarGenerate(w http.ResponseWriter, r *http.Request) {
	var p struct {
		AgentID  string                       `json:"agent_id"`
		Style    string                       `json:"style"`    // anime, realistic, pixel, abstract, minimalist
		Provider string                       `json:"provider"` // gemini
		Context  map[string]any `json:"context"` // identity, soul, behavior from browser state
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}
	if p.AgentID == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "agent_id required"})
		return
	}
	if p.Style == "" {
		p.Style = "minimalist"
	}

	// Build image prompt from request context (browser state) or fall back to disk files.
	agentDir := filepath.Join(s.agentsDir, p.AgentID)
	prompt := buildAvatarPrompt(agentDir, p.AgentID, p.Style, p.Context)

	imageData, err := generateViaGemini(r.Context(), prompt, s.store, s.encKey)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": "generation failed: " + err.Error()})
		return
	}

	// Save to agent directory.
	avatarPath := filepath.Join(agentDir, "avatar.png")
	os.MkdirAll(agentDir, 0755)
	if err := os.WriteFile(avatarPath, imageData, 0644); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": "saving avatar: " + err.Error()})
		return
	}

	dataURI := fmt.Sprintf("data:image/png;base64,%s", base64.StdEncoding.EncodeToString(imageData))

	writeJSON(w, map[string]any{
		"url":      fmt.Sprintf("/agents/%s/avatar.png", p.AgentID),
		"data_uri": dataURI,
		"provider": "gemini",
		"style":    p.Style,
	})
}

// buildAvatarPrompt creates an image generation prompt.
// Prefers context from the request body (browser state during creation);
// falls back to reading config files from disk (existing agents).
func buildAvatarPrompt(agentDir, agentID, style string, ctx map[string]any) string {
	var parts []string

	// Try request context first (sent by frontend during agent creation).
	if ctx != nil {
		for _, section := range []struct{ key, label string }{
			{"identity", "Identity"},
			{"soul", "Personality/Voice"},
			{"behavior", "Behavior"},
		} {
			if raw, ok := ctx[section.key]; ok {
				if desc := flattenContext(raw); desc != "" {
					parts = append(parts, section.label+": "+desc)
				}
			}
		}
	}

	// Fall back to disk files if no context from request.
	if len(parts) == 0 {
		if data, err := os.ReadFile(filepath.Join(agentDir, "identity.yaml")); err == nil {
			parts = append(parts, "Identity: "+string(data))
		}
		if data, err := os.ReadFile(filepath.Join(agentDir, "soul.md")); err == nil {
			soul := string(data)
			if len(soul) > 500 {
				soul = soul[:500]
			}
			parts = append(parts, "Personality: "+soul)
		}
		if data, err := os.ReadFile(filepath.Join(agentDir, "behavior.yaml")); err == nil {
			beh := string(data)
			if len(beh) > 500 {
				beh = beh[:500]
			}
			parts = append(parts, "Behavior: "+beh)
		}
	}

	agentDesc := strings.Join(parts, ". ")
	if agentDesc == "" {
		agentDesc = "An AI assistant named " + agentID
	}

	styleDesc := map[string]string{
		"anime":      "anime style, vibrant colors, expressive, digital art",
		"realistic":  "photorealistic, professional portrait, studio lighting",
		"pixel":      "pixel art, 16-bit retro game style, clean pixels",
		"abstract":   "abstract geometric art, modern, colorful shapes",
		"minimalist": "minimalist icon, flat design, single color, clean lines",
	}[style]
	if styleDesc == "" {
		styleDesc = "minimalist icon, flat design"
	}

	return fmt.Sprintf(
		"Generate an avatar image for an AI agent with the following information: %s. "+
			"Art style: %s. Square format, suitable as a profile picture. No text in the image.",
		agentDesc, styleDesc,
	)
}

// flattenContext converts an arbitrary JSON value into a readable string for the prompt.
func flattenContext(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case map[string]any:
		var parts []string
		for k, inner := range val {
			s := flattenContext(inner)
			if s != "" {
				parts = append(parts, k+": "+s)
			}
		}
		return strings.Join(parts, ", ")
	case []any:
		var items []string
		for _, item := range val {
			s := flattenContext(item)
			if s != "" {
				items = append(items, s)
			}
		}
		return strings.Join(items, ", ")
	case float64:
		return fmt.Sprintf("%g", val)
	case bool:
		return fmt.Sprintf("%v", val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// generateViaGemini calls Gemini's generateContent API for image generation.
// Uses gemini-2.0-flash-preview-image-generation for native image output.
func generateViaGemini(ctx context.Context, prompt string, store interface{ GetCredential(ctx context.Context, name string, encKey []byte) ([]byte, error) }, encKey []byte) ([]byte, error) {
	// Try env var first, then DB credential (provider_gemini_gemini is the default name).
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		if key, err := store.GetCredential(ctx, "provider_gemini_gemini", encKey); err == nil && len(key) > 0 {
			apiKey = string(key)
		}
	}
	if apiKey == "" {
		return nil, fmt.Errorf("Gemini API key not configured. Add a Gemini provider in Providers page.")
	}

	// Gemini generateContent API with native image generation.
	model := "gemini-2.5-flash-image"
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, apiKey)

	body, _ := json.Marshal(map[string]any{
		"contents": []map[string]any{{
			"parts": []map[string]any{{
				"text": prompt,
			}},
		}},
		"generationConfig": map[string]any{
			"responseModalities": []string{"TEXT", "IMAGE"},
		},
	})

	client := &http.Client{Timeout: 90 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API call failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody[:min(200, len(respBody))]))
	}

	// Parse generateContent response — image is in candidates[].content.parts[].inlineData.
	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					InlineData *struct {
						MimeType string `json:"mimeType"`
						Data     string `json:"data"`
					} `json:"inlineData,omitempty"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	// Find the image part in the response.
	for _, candidate := range result.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.InlineData != nil && strings.HasPrefix(part.InlineData.MimeType, "image/") {
				return base64.StdEncoding.DecodeString(part.InlineData.Data)
			}
		}
	}

	return nil, fmt.Errorf("no image in response")
}
