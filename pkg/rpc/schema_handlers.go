package rpc

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"github.com/xoai/sageclaw/pkg/agentcfg"
	"github.com/xoai/sageclaw/pkg/agentcfg/schemas"
	"github.com/xoai/sageclaw/pkg/canonical"
)

// handleSchemasList returns all available form schemas.
func (s *Server) handleSchemasList(w http.ResponseWriter, r *http.Request) {
	entries, err := fs.ReadDir(schemas.FS, ".")
	if err != nil {
		writeJSON(w, []any{})
		return
	}

	var schemaList []json.RawMessage
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := fs.ReadFile(schemas.FS, entry.Name())
		if err != nil {
			continue
		}
		schemaList = append(schemaList, json.RawMessage(data))
	}

	writeJSON(w, schemaList)
}

// handleSchemaGet returns a single form schema by type.
func (s *Server) handleSchemaGet(w http.ResponseWriter, r *http.Request) {
	schemaType := extractPathParam(r.URL.Path, "/api/v2/agents/schemas/")
	if schemaType == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "schema type required"})
		return
	}

	filename := schemaType + ".json"
	data, err := fs.ReadFile(schemas.FS, filename)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "schema not found: " + schemaType})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// handlePresetsList returns available agent presets.
func (s *Server) handlePresetsList(w http.ResponseWriter, r *http.Request) {
	presets := []map[string]string{
		{"id": "researcher", "name": "Researcher", "description": "Deep research, web search, knowledge graphs. Analytical and thorough.", "icon": "search"},
		{"id": "developer", "name": "Developer", "description": "Code writing, review, debugging. Precise and test-driven.", "icon": "code"},
		{"id": "writer", "name": "Writer", "description": "Content creation, editing, storytelling. Engaging and adaptable.", "icon": "pen"},
		{"id": "coordinator", "name": "Coordinator", "description": "Task management, delegation, synthesis. Organized and decisive.", "icon": "tasks"},
		{"id": "analyst", "name": "Analyst", "description": "Data analysis, pattern recognition, reporting. Detail-oriented.", "icon": "chart"},
		{"id": "assistant", "name": "Personal Assistant", "description": "General help, scheduling, reminders. Friendly and proactive.", "icon": "star"},
	}
	writeJSON(w, presets)
}

// handlePresetsApply returns a full agent config for a preset.
func (s *Server) handlePresetsApply(w http.ResponseWriter, r *http.Request) {
	presetID := extractPathParam(r.URL.Path, "/api/v2/agents/presets/")
	if presetID == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "preset ID required"})
		return
	}

	config := presetConfigs[presetID]
	if config == nil {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "preset not found: " + presetID})
		return
	}

	writeJSON(w, config)
}

// handleAgentGenerate uses LLM to generate agent config from a description.
// Enhanced with field-level validation, examples generation, and preset fallback.
func (s *Server) handleAgentGenerate(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil || p.Description == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "description required"})
		return
	}

	// Build the generation prompt.
	systemPrompt := `You are a SageClaw agent configuration generator. Given a user's description, generate a JSON agent config.

The output MUST be valid JSON — no markdown, no explanation, just the JSON object:
{
  "name": "string (1-50 chars, display name)",
  "role": "string (1-200 chars, what the agent does)",
  "avatar": "single emoji",
  "model": "strong|fast|local",
  "tool_profile": "full|coding|messaging|readonly|minimal",
  "examples": ["4-6 example prompts the user might try, each under 200 chars"]
}

Rules:
- name: short, memorable, no HTML tags
- role: concise description of capabilities
- avatar: exactly one emoji that represents the agent
- model: "strong" for complex tasks, "fast" for quick responses, "local" for privacy
- tool_profile: "full" (all tools), "coding" (dev focused), "messaging" (communication), "readonly" (safe browsing), "minimal" (conversation only)
- examples: 4-6 specific, actionable prompts a user would send to this agent

Generate a thoughtful agent that matches the user's description.`

	// If no router/provider configured, fall back to presets.
	if s.router == nil {
		writeJSON(w, map[string]any{
			"config":   bestPresetForDescription(p.Description),
			"fallback": true,
		})
		return
	}

	// Call LLM via the router using canonical format.
	ctx := r.Context()
	req := &canonical.Request{
		Model:     "strong",
		MaxTokens: 2048,
		System:    systemPrompt,
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: p.Description}}},
		},
	}

	resp, err := s.router.ChatWithFallback(ctx, "strong", req)
	if err != nil {
		// Fallback to presets on LLM failure.
		writeJSON(w, map[string]any{
			"config":   bestPresetForDescription(p.Description),
			"fallback": true,
			"reason":   "LLM unavailable, using preset",
		})
		return
	}

	// Extract text from response.
	response := ""
	if resp != nil {
		for _, msg := range resp.Messages {
			for _, c := range msg.Content {
				if c.Type == "text" {
					response += c.Text
				}
			}
		}
	}

	// Parse JSON from response (might have markdown wrapping).
	jsonStr := response
	if idx := strings.Index(jsonStr, "{"); idx >= 0 {
		depth := 0
		for i := idx; i < len(jsonStr); i++ {
			if jsonStr[i] == '{' {
				depth++
			}
			if jsonStr[i] == '}' {
				depth--
				if depth == 0 {
					jsonStr = jsonStr[idx : i+1]
					break
				}
			}
		}
	}

	// Validate it's valid JSON.
	var config map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &config); err != nil {
		// Fallback to presets on malformed JSON.
		writeJSON(w, map[string]any{
			"config":   bestPresetForDescription(p.Description),
			"fallback": true,
			"reason":   "Malformed LLM response, using preset",
		})
		return
	}

	// Sanitize and validate all fields.
	config = sanitizeGeneratedConfig(config)

	writeJSON(w, map[string]any{
		"config": config,
	})
}

// sanitizeGeneratedConfig validates and sanitizes LLM-generated agent config.
// Untrusted output — every field is checked and clamped.
func sanitizeGeneratedConfig(cfg map[string]any) map[string]any {
	result := make(map[string]any)

	// name: string, 1-50 chars, no HTML.
	result["name"] = sanitizeString(getStr(cfg, "name"), 50, "New Agent")

	// role: string, 1-200 chars, no HTML.
	result["role"] = sanitizeString(getStr(cfg, "role"), 200, "AI assistant")

	// avatar: 1-10 chars (emoji).
	avatar := getStr(cfg, "avatar")
	if avatar == "" || len(avatar) > 30 { // emojis can be multi-byte
		avatar = "\u2B50"
	}
	result["avatar"] = avatar

	// model: must be one of known values.
	model := getStr(cfg, "model")
	validModels := map[string]bool{"strong": true, "fast": true, "local": true}
	if !validModels[model] {
		model = "strong"
	}
	result["model"] = model

	// tool_profile: must be one of known values.
	profile := getStr(cfg, "tool_profile")
	validProfiles := map[string]bool{"full": true, "coding": true, "messaging": true, "readonly": true, "minimal": true}
	if !validProfiles[profile] {
		profile = "full"
	}
	result["tool_profile"] = profile

	// examples: array of 0-6 strings, each max 200 chars.
	result["examples"] = sanitizeExamples(cfg)

	return result
}

// sanitizeExamples extracts and sanitizes example prompts from LLM output.
func sanitizeExamples(cfg map[string]any) []string {
	raw, ok := cfg["examples"]
	if !ok {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	var examples []string
	for _, item := range arr {
		s, ok := item.(string)
		if !ok || s == "" {
			continue
		}
		s = stripHTMLTags(s)
		if len(s) > 200 {
			s = s[:200]
		}
		examples = append(examples, s)
		if len(examples) >= 6 {
			break
		}
	}
	return examples
}

// bestPresetForDescription picks the best preset archetype based on keywords.
func bestPresetForDescription(desc string) map[string]any {
	lower := strings.ToLower(desc)

	type match struct {
		id       string
		keywords []string
	}
	matches := []match{
		{"developer", []string{"code", "program", "develop", "debug", "software", "engineer"}},
		{"researcher", []string{"research", "search", "find", "discover", "investigate", "study"}},
		{"writer", []string{"write", "blog", "content", "article", "story", "edit", "draft"}},
		{"analyst", []string{"analy", "data", "report", "chart", "metric", "statistic"}},
		{"coordinator", []string{"manage", "coordinate", "delegate", "team", "project", "task"}},
	}

	for _, m := range matches {
		for _, kw := range m.keywords {
			if strings.Contains(lower, kw) {
				preset := presetConfigs[m.id]
				if preset != nil {
					// Convert preset to simplified format.
					identity, _ := preset["identity"].(map[string]any)
					return map[string]any{
						"name":         identity["name"],
						"role":         identity["purpose"],
						"avatar":       identity["emoji"],
						"model":        identity["model"],
						"tool_profile": "full",
						"examples":     agentcfg.DefaultExamples("full"),
					}
				}
			}
		}
	}

	// Default: assistant preset.
	return map[string]any{
		"name":         "Assistant",
		"role":         "General-purpose AI assistant",
		"avatar":       "\u2B50",
		"model":        "strong",
		"tool_profile": "full",
		"examples":     agentcfg.DefaultExamples("full"),
	}
}

// getStr safely extracts a string from a map.
func getStr(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// sanitizeString strips HTML tags and clamps length.
func sanitizeString(s string, maxLen int, fallback string) string {
	s = stripHTMLTags(s)
	if s == "" {
		return fallback
	}
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return s
}

// stripHTMLTags removes HTML/script tags from a string.
func stripHTMLTags(s string) string {
	var result strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// presetConfigs defines the full config for each preset archetype.
var presetConfigs = map[string]map[string]any{
	"researcher": {
		"identity": map[string]any{"name": "Researcher", "role": "Research analyst", "creature": "Knowledge Oracle", "model": "strong", "max_tokens": "8192", "status": "active", "emoji": "\U0001F50D", "purpose": "Deep research, analysis, and knowledge building."},
		"soul":     map[string]any{"tone": "Professional", "humor": "Subtle", "emoji_usage": "Sparingly", "response_length": "Detailed", "formality": "Professional", "opinions": true, "core_values": []string{"Accuracy", "Thoroughness", "Transparency"}, "boundaries": []string{"Won't share user data"}, "expertise": "Web research, data analysis, literature review, competitive analysis, trend identification."},
		"behavior": map[string]any{"no_parrot": true, "no_pad": true, "answer_first": true, "match_energy": true, "short_ok": true, "decision_style": "Balanced", "error_handling": "Retry once then ask"},
		"skills":   map[string]any{"professional_skills": []string{"Research & Analysis", "Technical Writing", "Data Analysis"}},
		"tools":    map[string]any{"enabled": []string{"web_search", "web_fetch", "memory_search", "memory_store", "memory_link", "memory_graph", "read_file", "write_file", "list_directory"}},
		"memory":   map[string]any{"scope": "project", "auto_save": true, "search_on_start": true, "retention_days": "180", "compaction_enabled": true, "compaction_threshold": "50"},
		"heartbeat": map[string]any{"tasks": []any{}},
		"channels":  map[string]any{"serve": []string{"cli", "web"}, "default_channel": "web"},
		"bootstrap": map[string]any{"enabled": true, "greeting": "Hi! I'm your research assistant. I specialize in finding information, analyzing data, and building knowledge graphs.\n\nTell me what you'd like to research, and I'll get started.", "discovery_questions": []string{"What's your name?", "What topic or project are you researching?", "How detailed should my reports be?"}},
	},
	"developer": {
		"identity": map[string]any{"name": "Developer", "role": "Software engineer", "creature": "Code Golem", "model": "strong", "max_tokens": "8192", "status": "active", "emoji": "\U0001F4BB", "purpose": "Code writing, review, debugging, and software architecture."},
		"soul":     map[string]any{"tone": "Direct", "humor": "Subtle", "emoji_usage": "Never", "response_length": "Moderate", "formality": "Casual", "opinions": true, "core_values": []string{"Accuracy", "Efficiency", "Thoroughness"}, "boundaries": []string{"Won't share user data", "Won't modify system files"}, "expertise": "Full-stack development, code review, debugging, testing, system design, API development."},
		"behavior": map[string]any{"no_parrot": true, "no_pad": true, "answer_first": true, "match_energy": true, "short_ok": true, "decision_style": "Bold", "error_handling": "Try alternative approach"},
		"skills":   map[string]any{"professional_skills": []string{"Code Review", "Testing & QA", "System Design", "API Development"}},
		"tools":    map[string]any{"enabled": []string{"read_file", "write_file", "list_directory", "execute_command", "web_search", "web_fetch", "memory_search", "memory_store"}},
		"memory":   map[string]any{"scope": "project", "auto_save": true, "search_on_start": false, "retention_days": "90", "compaction_enabled": true, "compaction_threshold": "50"},
		"heartbeat": map[string]any{"tasks": []any{}},
		"channels":  map[string]any{"serve": []string{"cli", "web"}, "default_channel": "cli"},
		"bootstrap": map[string]any{"enabled": true, "greeting": "Hey. I'm your development agent. I write code, review PRs, debug issues, and design systems.\n\nWhat are we building?", "discovery_questions": []string{"What's your name?", "What language/stack are you working with?", "Any coding conventions I should follow?"}},
	},
	"writer": {
		"identity": map[string]any{"name": "Writer", "role": "Content creator", "creature": "Digital Companion", "model": "strong", "max_tokens": "16384", "status": "active", "emoji": "\u270D\uFE0F", "purpose": "Content creation, editing, storytelling across all formats."},
		"soul":     map[string]any{"tone": "Warm", "humor": "Natural", "emoji_usage": "Sparingly", "response_length": "Comprehensive", "formality": "Casual", "opinions": true, "core_values": []string{"Creativity", "Empathy", "Accuracy"}, "boundaries": []string{"Won't share user data"}, "expertise": "Blog posts, articles, newsletters, social media, technical writing, storytelling, SEO."},
		"behavior": map[string]any{"no_parrot": true, "no_pad": true, "answer_first": false, "match_energy": true, "short_ok": false, "decision_style": "Balanced", "error_handling": "Retry once then ask"},
		"skills":   map[string]any{"professional_skills": []string{"Content Creation", "SEO & Marketing", "Technical Writing"}},
		"tools":    map[string]any{"enabled": []string{"web_search", "web_fetch", "read_file", "write_file", "memory_search", "memory_store"}},
		"memory":   map[string]any{"scope": "project", "auto_save": true, "search_on_start": true, "retention_days": "180", "compaction_enabled": true, "compaction_threshold": "75"},
		"heartbeat": map[string]any{"tasks": []any{}},
		"channels":  map[string]any{"serve": []string{"cli", "web"}, "default_channel": "web"},
		"bootstrap": map[string]any{"enabled": true, "greeting": "Hi! I'm your writing partner. I help with blog posts, articles, newsletters, social media — anything that needs words.\n\nWhat are we writing today?", "discovery_questions": []string{"What's your name?", "What kind of content do you usually create?", "What tone works best for your audience?"}},
	},
	"coordinator": {
		"identity": map[string]any{"name": "Coordinator", "role": "Team lead and orchestrator", "creature": "Task Automaton", "model": "fast", "max_tokens": "4096", "status": "active", "emoji": "\U0001F3AF", "purpose": "Break down complex tasks, delegate to specialists, synthesize results."},
		"soul":     map[string]any{"tone": "Professional", "humor": "None", "emoji_usage": "Never", "response_length": "Concise", "formality": "Professional", "opinions": true, "core_values": []string{"Efficiency", "Transparency", "Thoroughness"}, "boundaries": []string{"Won't share user data", "Asks before external actions"}, "expertise": "Task decomposition, project management, delegation, synthesis."},
		"behavior": map[string]any{"no_parrot": true, "no_pad": true, "answer_first": true, "match_energy": false, "short_ok": true, "decision_style": "Autonomous", "error_handling": "Report and continue"},
		"skills":   map[string]any{"professional_skills": []string{"Project Management", "Research & Analysis"}},
		"tools":    map[string]any{"enabled": []string{"delegate", "delegation_status", "team_create_task", "team_assign_task", "team_list_tasks", "memory_search", "memory_store", "memory_link", "handoff"}},
		"memory":   map[string]any{"scope": "project", "auto_save": true, "search_on_start": true, "retention_days": "90", "compaction_enabled": true, "compaction_threshold": "50"},
		"heartbeat": map[string]any{"tasks": []any{}},
		"channels":  map[string]any{"serve": []string{"cli", "web", "telegram"}, "default_channel": "web"},
		"bootstrap": map[string]any{"enabled": true, "greeting": "I'm the Coordinator. I break down complex tasks, delegate to specialist agents, and deliver synthesized results.\n\nWhat do you need done?", "discovery_questions": []string{"What's your name?", "What kind of work do you need help with?"}},
	},
	"analyst": {
		"identity": map[string]any{"name": "Analyst", "role": "Data and research analyst", "creature": "Knowledge Oracle", "model": "strong", "max_tokens": "8192", "status": "active", "emoji": "\U0001F4CA", "purpose": "Data analysis, pattern recognition, insight generation, reporting."},
		"soul":     map[string]any{"tone": "Professional", "humor": "None", "emoji_usage": "Never", "response_length": "Detailed", "formality": "Professional", "opinions": true, "core_values": []string{"Accuracy", "Transparency", "Thoroughness"}, "boundaries": []string{"Won't share user data"}, "expertise": "Statistical analysis, trend identification, competitive analysis, financial modeling, data visualization."},
		"behavior": map[string]any{"no_parrot": true, "no_pad": true, "answer_first": true, "match_energy": false, "short_ok": false, "decision_style": "Cautious", "error_handling": "Stop and ask the user"},
		"skills":   map[string]any{"professional_skills": []string{"Data Analysis", "Financial Analysis", "Research & Analysis"}},
		"tools":    map[string]any{"enabled": []string{"web_search", "web_fetch", "read_file", "write_file", "memory_search", "memory_store", "memory_link", "memory_graph", "execute_command"}},
		"memory":   map[string]any{"scope": "project", "auto_save": true, "search_on_start": true, "retention_days": "365", "compaction_enabled": true, "compaction_threshold": "50"},
		"heartbeat": map[string]any{"tasks": []any{}},
		"channels":  map[string]any{"serve": []string{"cli", "web"}, "default_channel": "web"},
		"bootstrap": map[string]any{"enabled": true, "greeting": "I'm your analyst. I specialize in data analysis, pattern recognition, and generating actionable insights.\n\nWhat data or question would you like me to analyze?", "discovery_questions": []string{"What's your name?", "What domain are you working in?", "What kind of reports do you prefer?"}},
	},
	"assistant": {
		"identity": map[string]any{"name": "Assistant", "role": "Personal AI assistant", "creature": "Digital Companion", "model": "fast", "max_tokens": "4096", "status": "active", "emoji": "\u2B50", "purpose": "General help with daily tasks, scheduling, reminders, and quick answers."},
		"soul":     map[string]any{"tone": "Friendly", "humor": "Natural", "emoji_usage": "Moderate", "response_length": "Concise", "formality": "Casual", "opinions": true, "core_values": []string{"Efficiency", "Empathy", "Resourcefulness"}, "boundaries": []string{"Won't share user data", "Asks before external actions"}, "expertise": "General knowledge, task management, scheduling, quick research, reminders."},
		"behavior": map[string]any{"no_parrot": true, "no_pad": true, "answer_first": true, "match_energy": true, "short_ok": true, "decision_style": "Balanced", "error_handling": "Retry once then ask"},
		"skills":   map[string]any{"professional_skills": []string{"Project Management"}},
		"tools":    map[string]any{"enabled": []string{"web_search", "web_fetch", "read_file", "write_file", "list_directory", "execute_command", "memory_search", "memory_store", "cron_create", "cron_list"}},
		"memory":   map[string]any{"scope": "project", "auto_save": true, "search_on_start": true, "retention_days": "90", "compaction_enabled": true, "compaction_threshold": "50"},
		"heartbeat": map[string]any{"tasks": []any{}},
		"channels":  map[string]any{"serve": []string{"cli", "web", "telegram"}, "default_channel": "web"},
		"bootstrap": map[string]any{"enabled": true, "greeting": "Hey! I'm your personal assistant. I help with daily tasks, research, scheduling, and anything else you need.\n\nWhat can I help you with?", "discovery_questions": []string{"What's your name?", "What's your timezone?", "How should I communicate — formal or casual?"}},
	},
}
