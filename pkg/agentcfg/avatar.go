package agentcfg

import "strings"

// AutoAvatar picks an emoji avatar based on the agent's name and role.
// Returns empty string if no match found (caller should use a fallback).
func AutoAvatar(name, role string) string {
	text := strings.ToLower(name + " " + role)

	// Check in priority order — first match wins.
	for _, m := range avatarKeywords {
		for _, kw := range m.keywords {
			if strings.Contains(text, kw) {
				return m.emoji
			}
		}
	}

	// Fallback: pick based on first letter of name.
	if name != "" {
		r := []rune(strings.ToUpper(name))[0]
		if r >= 'A' && r <= 'Z' {
			return letterAvatars[r-'A']
		}
	}

	return "\u2B50" // star fallback
}

type avatarMatch struct {
	emoji    string
	keywords []string
}

var avatarKeywords = []avatarMatch{
	// Research & analysis
	{"\U0001F50D", []string{"research", "search", "investigat", "analyst", "analys"}},
	// Code & development
	{"\U0001F4BB", []string{"code", "coding", "develop", "software", "engineer", "program", "debug"}},
	// Writing & content
	{"\u270D\uFE0F", []string{"writ", "content", "blog", "article", "editor", "copy", "story"}},
	// Data & charts
	{"\U0001F4CA", []string{"data", "chart", "metric", "statistic", "report", "dashboard"}},
	// Design & creative
	{"\U0001F3A8", []string{"design", "creative", "art", "visual", "ui", "ux", "graphic"}},
	// Teaching & learning
	{"\U0001F4DA", []string{"teach", "tutor", "learn", "education", "mentor", "coach"}},
	// Science & lab
	{"\U0001F52C", []string{"scien", "lab", "experiment", "physics", "chemistry", "biology"}},
	// Math & calculation
	{"\U0001F9EE", []string{"math", "calcul", "number", "accounti", "financ"}},
	// Legal
	{"\u2696\uFE0F", []string{"legal", "law", "attorney", "complia", "regulat"}},
	// Medical & health
	{"\U0001FA7A", []string{"medic", "health", "doctor", "clinic", "diagnos", "patient"}},
	// Communication & support
	{"\U0001F4AC", []string{"support", "customer", "help desk", "service", "communi"}},
	// Marketing & sales
	{"\U0001F4E3", []string{"market", "sales", "adverti", "campaign", "promot", "seo"}},
	// Project management & coordination
	{"\U0001F3AF", []string{"project", "manag", "coordinat", "plan", "organiz", "task"}},
	// Security
	{"\U0001F6E1\uFE0F", []string{"secur", "cyber", "protect", "privacy", "encrypt"}},
	// Translation & language
	{"\U0001F310", []string{"translat", "language", "locali", "i18n", "interpret"}},
	// Music & audio
	{"\U0001F3B5", []string{"music", "audio", "sound", "podcast", "voice"}},
	// Video & media
	{"\U0001F3AC", []string{"video", "media", "film", "stream", "broadcast"}},
	// Shopping & commerce
	{"\U0001F6D2", []string{"shop", "commerce", "ecommerce", "retail", "product"}},
	// Travel & maps
	{"\U0001F5FA\uFE0F", []string{"travel", "map", "location", "geograph", "navigat"}},
	// Food & cooking
	{"\U0001F373", []string{"food", "cook", "recipe", "chef", "culinar", "nutrition"}},
	// Sports & fitness
	{"\U0001F3C3", []string{"sport", "fitness", "exercise", "athlet", "workout"}},
	// General assistant (low priority)
	{"\U0001F916", []string{"assistant", "bot", "agent", "helper", "general"}},
}

// Letter-based fallback avatars — visually distinct per letter.
var letterAvatars = [26]string{
	"\U0001F170\uFE0F", // A
	"\U0001F171\uFE0F", // B
	"\u00A9\uFE0F",     // C
	"\u2666\uFE0F",     // D
	"\U0001F4A7",       // E (droplet)
	"\U0001F525",       // F (fire)
	"\U0001F48E",       // G (gem)
	"\u2665\uFE0F",     // H
	"\u2139\uFE0F",     // I
	"\U0001F0CF",       // J (joker)
	"\U0001F511",       // K (key)
	"\u26A1",           // L (lightning)
	"\u2649",           // M
	"\U0001F4AB",       // N (dizzy star)
	"\u2B55",           // O
	"\u2734\uFE0F",     // P
	"\u2753",           // Q
	"\U0001F308",       // R (rainbow)
	"\u2B50",           // S (star)
	"\U0001F3C6",       // T (trophy)
	"\u2602\uFE0F",     // U
	"\u2714\uFE0F",     // V
	"\U0001F30A",       // W (wave)
	"\u2716\uFE0F",     // X
	"\U0001F49B",       // Y (yellow heart)
	"\u26A1",           // Z (zap)
}
