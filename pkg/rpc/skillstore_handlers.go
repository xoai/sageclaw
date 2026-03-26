package rpc

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// handleSkillMarketplaceSearch proxies search to skills.sh API.
func (s *Server) handleSkillMarketplaceSearch(w http.ResponseWriter, r *http.Request) {
	if s.skillStore == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, map[string]string{"error": "skill store not configured"})
		return
	}

	query := r.URL.Query().Get("q")
	if query == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "q parameter required"})
		return
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := json.Number(l).Int64(); err == nil && n > 0 && n <= 100 {
			limit = int(n)
		}
	}

	results, err := s.skillStore.Search(r.Context(), query, limit)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		writeJSON(w, map[string]string{"error": "search failed: " + err.Error()})
		return
	}

	writeJSON(w, map[string]any{
		"query":   query,
		"results": results,
		"count":   len(results),
	})
}

// handleSkillMarketplacePreview fetches skill info for consent review.
func (s *Server) handleSkillMarketplacePreview(w http.ResponseWriter, r *http.Request) {
	if s.skillStore == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, map[string]string{"error": "skill store not configured"})
		return
	}

	source := r.URL.Query().Get("source")
	if source == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "source parameter required"})
		return
	}

	preview, err := s.skillStore.Preview(r.Context(), source)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		writeJSON(w, map[string]string{"error": "preview failed: " + err.Error()})
		return
	}

	writeJSON(w, preview)
}

// handleSkillMarketplaceInstall installs a skill after user consent.
func (s *Server) handleSkillMarketplaceInstall(w http.ResponseWriter, r *http.Request) {
	if s.skillStore == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, map[string]string{"error": "skill store not configured"})
		return
	}

	var req struct {
		Source   string   `json:"source"`
		Approved bool     `json:"approved"`
		Agents   []string `json:"agents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Source == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "source is required"})
		return
	}
	if !req.Approved {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "installation requires user approval (approved: true)"})
		return
	}

	installed, err := s.skillStore.Install(r.Context(), req.Source)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": "install failed: " + err.Error()})
		return
	}

	// Assign to agents if specified.
	if len(req.Agents) > 0 && s.agentsDir != "" {
		for _, agentID := range req.Agents {
			if !validateAgentID(agentID) {
				log.Printf("skillstore: invalid agent ID %q, skipping", agentID)
				continue
			}
			if err := addSkillToAgent(s.agentsDir, agentID, installed.Name); err != nil {
				log.Printf("skillstore: failed to assign %s to agent %s: %v", installed.Name, agentID, err)
			}
		}
		installed.Agents = req.Agents
	}

	sendSIGHUP()
	writeJSON(w, installed)
}

// handleSkillMarketplaceInstalled lists all installed marketplace skills.
func (s *Server) handleSkillMarketplaceInstalled(w http.ResponseWriter, r *http.Request) {
	if s.skillStore == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, map[string]string{"error": "skill store not configured"})
		return
	}

	skills := s.skillStore.Installed()

	// Enrich with agent assignments.
	if s.agentsDir != "" {
		assignments := loadAllAgentSkillAssignments(s.agentsDir)
		for i, sk := range skills {
			for agentID, agentSkills := range assignments {
				for _, name := range agentSkills {
					if name == sk.Name {
						skills[i].Agents = append(skills[i].Agents, agentID)
					}
				}
			}
		}
	}

	writeJSON(w, skills)
}

// handleSkillMarketplaceUninstall removes a skill.
func (s *Server) handleSkillMarketplaceUninstall(w http.ResponseWriter, r *http.Request) {
	if s.skillStore == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, map[string]string{"error": "skill store not configured"})
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/skills/marketplace/")
	if name == "" || strings.Contains(name, "..") || strings.Contains(name, "/") {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid skill name"})
		return
	}

	// Remove from all agents first.
	if s.agentsDir != "" {
		removeSkillFromAllAgents(s.agentsDir, name)
	}

	if err := s.skillStore.Uninstall(name); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	sendSIGHUP()
	writeJSON(w, map[string]string{"status": "uninstalled", "name": name})
}

// handleSkillMarketplaceUpdate updates a specific skill.
func (s *Server) handleSkillMarketplaceUpdate(w http.ResponseWriter, r *http.Request) {
	if s.skillStore == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, map[string]string{"error": "skill store not configured"})
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/skills/marketplace/update/")
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "skill name required"})
		return
	}

	updated, err := s.skillStore.Update(r.Context(), name)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	sendSIGHUP()
	writeJSON(w, updated)
}

// handleSkillMarketplaceUpdates checks all skills for available updates.
func (s *Server) handleSkillMarketplaceUpdates(w http.ResponseWriter, r *http.Request) {
	if s.skillStore == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, map[string]string{"error": "skill store not configured"})
		return
	}

	updates, err := s.skillStore.CheckUpdates(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]any{
		"updates": updates,
		"count":   len(updates),
	})
}

// handleSkillMarketplaceAssign assigns/unassigns a skill to agents.
func (s *Server) handleSkillMarketplaceAssign(w http.ResponseWriter, r *http.Request) {
	if s.agentsDir == "" {
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, map[string]string{"error": "agents directory not configured"})
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/skills/marketplace/assign/")
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "skill name required"})
		return
	}

	var req struct {
		Agents []string `json:"agents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request body"})
		return
	}

	// Build desired set.
	desired := make(map[string]bool)
	for _, a := range req.Agents {
		desired[a] = true
	}

	// Get all agents and update skills.yaml for each.
	entries, err := os.ReadDir(s.agentsDir)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": "read agents dir: " + err.Error()})
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		agentID := entry.Name()
		if desired[agentID] {
			addSkillToAgent(s.agentsDir, agentID, name)
		} else {
			removeSkillFromAgent(s.agentsDir, agentID, name)
		}
	}

	sendSIGHUP()
	writeJSON(w, map[string]any{
		"skill":  name,
		"agents": req.Agents,
	})
}

// --- helpers ---

type skillsYAML struct {
	Skills []string `yaml:"skills"`
}

func loadAgentSkills(agentsDir, agentID string) []string {
	path := filepath.Join(agentsDir, agentID, "skills.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg skillsYAML
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	return cfg.Skills
}

func saveAgentSkills(agentsDir, agentID string, skills []string) error {
	cfg := skillsYAML{Skills: skills}
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}
	path := filepath.Join(agentsDir, agentID, "skills.yaml")
	return os.WriteFile(path, data, 0o644)
}

func addSkillToAgent(agentsDir, agentID, skillName string) error {
	skills := loadAgentSkills(agentsDir, agentID)
	for _, s := range skills {
		if s == skillName {
			return nil
		}
	}
	skills = append(skills, skillName)
	return saveAgentSkills(agentsDir, agentID, skills)
}

func removeSkillFromAgent(agentsDir, agentID, skillName string) error {
	skills := loadAgentSkills(agentsDir, agentID)
	var filtered []string
	for _, s := range skills {
		if s != skillName {
			filtered = append(filtered, s)
		}
	}
	if len(filtered) == len(skills) {
		return nil
	}
	return saveAgentSkills(agentsDir, agentID, filtered)
}

func removeSkillFromAllAgents(agentsDir, skillName string) {
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			removeSkillFromAgent(agentsDir, entry.Name(), skillName)
		}
	}
}

func loadAllAgentSkillAssignments(agentsDir string) map[string][]string {
	assignments := make(map[string][]string)
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return assignments
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skills := loadAgentSkills(agentsDir, entry.Name())
		if len(skills) > 0 {
			assignments[entry.Name()] = skills
		}
	}
	return assignments
}

func sendSIGHUP() {
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		return
	}
	sendHUPSignal(p)
}
