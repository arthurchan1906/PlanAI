package proxy

import (
	pmdb "aipmc/db"
)

// loadCurrentModel reads the per-agent override for the given agent.
// Returns "" when no override (Auto mode / passthrough).
func loadCurrentModel(agent string) string {
	return pmdb.LoadCurrentModel(agent)
}

// saveCurrentModel persists a per-agent model override.
func saveCurrentModel(agent, model string) error {
	return pmdb.SaveCurrentModel(agent, model)
}