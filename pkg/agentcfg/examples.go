package agentcfg

// DefaultExamples returns static example prompts based on the agent's tool profile.
// Used as fallback when an agent has no custom examples in identity.yaml.
func DefaultExamples(profile string) []string {
	if examples, ok := profileExamples[profile]; ok {
		return examples
	}
	return profileExamples["full"]
}

var profileExamples = map[string][]string{
	"full": {
		"Research the latest trends in AI safety",
		"Write a blog post about Go performance tips",
		"Analyze this CSV data and summarize the key findings",
		"Help me debug a failing test in my project",
	},
	"coding": {
		"Write a function that parses JSON logs and extracts errors",
		"Refactor this code to use the strategy pattern",
		"Help me debug why my API returns 500 on POST requests",
		"Write unit tests for the authentication module",
	},
	"messaging": {
		"Draft a friendly reply to this customer complaint",
		"Summarize the key points from today's meeting",
		"Write a project status update for the team",
		"Help me explain this technical decision to stakeholders",
	},
	"readonly": {
		"Explain how the authentication flow works in this codebase",
		"Find all API endpoints and list their HTTP methods",
		"Summarize what changed in the last 10 commits",
		"Search for potential security issues in the config files",
	},
	"minimal": {
		"Explain the difference between goroutines and threads",
		"Help me understand this error message",
		"What are the best practices for REST API design?",
		"Suggest a data structure for a task queue",
	},
}

// ResolveExamples returns the agent's examples, falling back to profile defaults.
func ResolveExamples(cfg *AgentConfig) []string {
	if len(cfg.Identity.Examples) > 0 {
		return cfg.Identity.Examples
	}
	return DefaultExamples(cfg.Tools.Profile)
}
