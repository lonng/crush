package exports

import "github.com/charmbracelet/crush/internal/config"

func init() {
	config.SetAppName("loop")
	config.SetDefaultDataDirectory(".loop")
	config.SetDefaultInitializeAs("AGENTS.md")
	config.SetDefaultContextPaths([]string{"AGENTS.md", "CLAUDE.md"})
}
