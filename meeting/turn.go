package meeting

import (
	"aipmc/store"
)

// SetPMTyping updates the pm_typing flag on a meeting room.
func SetPMTyping(roomID string, typing bool) error {
	return store.SetMeetingPMTyping(roomID, typing)
}

// AgentSpeak records voluntary agent speech (already responded).
func AgentSpeak(roomID, agentID, content, replyTo, addressTo string) (map[string]any, error) {
	return store.CreateAgentVoluntaryTurn(roomID, agentID, content, replyTo, addressTo)
}

// NameAgent creates a turn asking agentID to speak (PM 点名).
func NameAgent(roomID, agentID, question string) (map[string]any, error) {
	return store.CreateMeetingTurn(roomID, 0, "agent", agentID, question)
}

// PMMessage records a PM message turn.
func PMMessage(roomID, text string) (map[string]any, error) {
	return store.CreateMeetingTurn(roomID, 0, "human", "PM", text)
}
