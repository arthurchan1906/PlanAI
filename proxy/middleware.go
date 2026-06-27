package proxy

// =============================================================================
// AIPM Middleware — placeholder interface for content intervention (目的 2)
// =============================================================================

// AIPMMiddleware intercepts the unified request/response pipeline to inject
// AIPM project management context. Currently a no-op placeholder; the actual
// enrichment logic will be implemented later.
//
// Purpose: all code agents passing through this proxy automatically receive
// project brief, task context, and knowledge aggregation without per-agent
// configuration.
type AIPMMiddleware interface {
	// EnrichRequest injects AIPM context (briefing, task info, etc.) into
	// the request before it is sent upstream.
	EnrichRequest(req *UnifiedReq)

	// PostProcessResponse applies AIPM post-processing (e.g., recording
	// decisions, extracting knowledge) to the streamed response events.
	PostProcessResponse(events []UnifiedStreamEvent) []UnifiedStreamEvent
}

// noopMiddleware is the default middleware that passes everything through unchanged.
type noopMiddleware struct{}

func (n *noopMiddleware) EnrichRequest(req *UnifiedReq) {}

func (n *noopMiddleware) PostProcessResponse(events []UnifiedStreamEvent) []UnifiedStreamEvent {
	return events
}

// DefaultMiddleware returns a no-op AIPM middleware. Replace with actual
// implementation when purpose 2 is implemented.
func DefaultMiddleware() AIPMMiddleware {
	return &noopMiddleware{}
}
