package agent

// StreamCallbacks receives incremental agent output for web streaming.
type StreamCallbacks struct {
	OnToken      func(token string)
	OnToolStart  func(id, name string, args map[string]any)
	OnToolResult func(id, name, result string)
}

// RunStream processes user input like Run but streams text tokens when callbacks are set.
func (a *Agent) RunStream(s *Session, userInput string, cb *StreamCallbacks) (string, error) {
	return a.runSession(s, userInput, cb)
}
