package rpc

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

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
	toolList := []string{"read_file", "write_file", "list_directory", "execute_command",
		"web_search", "web_fetch", "memory_search", "memory_store", "memory_delete",
		"memory_link", "memory_graph", "cron_create", "cron_list", "cron_delete",
		"delegate", "delegation_status", "team_create_task", "team_assign_task",
		"team_claim_task", "team_complete_task", "team_list_tasks", "team_send",
		"team_inbox", "handoff", "evaluate", "spawn", "audit_search", "audit_stats",
		"credential_store", "credential_get"}

	systemPrompt := `You are a SageClaw agent configuration generator. Given a user's description of what they want their agent to do, generate a complete agent configuration as JSON.

The output MUST be valid JSON matching this exact structure — no markdown, no explanation, just the JSON object:
{
  "identity": { "name": "string", "role": "string", "creature": "AI Assistant|Digital Companion|Research Familiar|Code Golem|Knowledge Oracle|Task Automaton", "model": "strong|fast|local", "max_tokens": "4096|8192|16384", "status": "active", "emoji": "single emoji", "purpose": "one paragraph" },
  "soul": { "tone": "Professional|Casual|Friendly|Formal|Witty|Warm|Direct|Empathetic", "humor": "None|Subtle|Natural|Frequent", "emoji_usage": "Never|Sparingly|Moderate|Frequently", "response_length": "Concise|Moderate|Detailed|Comprehensive", "formality": "Very Formal|Professional|Casual|Very Casual", "opinions": true/false, "core_values": ["from: Accuracy,Transparency,Efficiency,Creativity,Empathy,Privacy,Thoroughness,Resourcefulness,Honesty,Curiosity"], "boundaries": ["from: Won't share user data,Won't make purchases,Won't send external messages without asking,Won't modify system files,Asks before external actions"], "expertise": "string", "core_truths": "string" },
  "behavior": { "no_parrot": true/false, "no_pad": true/false, "answer_first": true/false, "match_energy": true/false, "short_ok": true/false, "decision_style": "Cautious|Balanced|Bold|Autonomous", "error_handling": "Stop and ask|Retry once then ask|Try alternative|Report and continue", "custom_rules": "string" },
  "skills": { "professional_skills": ["from: Research & Analysis,Technical Writing,Code Review,Data Analysis,Project Management,Content Creation,SEO & Marketing,System Design,Database Management,API Development,Testing & QA,DevOps,UI/UX Design,Financial Analysis,Legal Research,Translation,Teaching & Tutoring"], "processes": "string" },
  "tools": { "enabled": ["from available tools list"] },
  "memory": { "scope": "project|global", "auto_save": true/false, "search_on_start": true/false, "retention_days": "30|60|90|180|365", "compaction_enabled": true/false, "compaction_threshold": "30|50|75|100" },
  "heartbeat": { "tasks": [] },
  "channels": { "serve": ["cli", "web"], "default_channel": "web" },
  "bootstrap": { "enabled": true/false, "greeting": "string", "discovery_questions": ["string"], "auto_delete": true }
}

Available tools: ` + strings.Join(toolList, ", ") + `

Generate a thoughtful, well-configured agent that matches the user's description. Be specific in the soul, expertise, and behavior sections.`

	// Estimate cost (rough: ~1500 input tokens + ~500 output tokens).
	costEstimate := "$0.01-0.03"

	// If no router/provider configured, return error.
	if s.router == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, map[string]string{"error": "No AI provider configured. Add a provider first."})
		return
	}

	// Call LLM via the router using canonical format.
	ctx := r.Context()
	req := &canonical.Request{
		Model:     "strong",
		MaxTokens: 4096,
		System:    systemPrompt,
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: p.Description}}},
		},
	}

	resp, err := s.router.ChatWithFallback(ctx, "strong", req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": "Generation failed: " + err.Error()})
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
		// Find matching closing brace.
		depth := 0
		for i := idx; i < len(jsonStr); i++ {
			if jsonStr[i] == '{' { depth++ }
			if jsonStr[i] == '}' { depth--; if depth == 0 { jsonStr = jsonStr[idx:i+1]; break } }
		}
	}

	// Validate it's valid JSON.
	var config map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &config); err != nil {
		// Return raw response for debugging.
		writeJSON(w, map[string]any{
			"error": "Failed to parse generated config. Try regenerating.",
			"raw": response,
			"cost_estimate": costEstimate,
		})
		return
	}

	writeJSON(w, map[string]any{
		"config": config,
		"cost_estimate": costEstimate,
	})
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
