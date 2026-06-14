package meeting

import (
	"fmt"

	"aipmc/ai"
	"aipmc/store"
	"aipmc/u"
)

// ArbitrateResult is returned when AI picks the next speaker.
type ArbitrateResult struct {
	NextAgent string         `json:"next_agent"`
	Reason    string         `json:"reason"`
	Turn      map[string]any `json:"turn,omitempty"`
}

// ArbitrateNext uses AI to pick the next agent and creates a waiting turn.
func ArbitrateNext(client *ai.Client, roomID string) (*ArbitrateResult, error) {
	if client == nil || !client.Enabled() {
		return nil, fmt.Errorf("AI not configured")
	}
	room, err := store.GetMeetingRoom(roomID)
	if err != nil {
		return nil, err
	}
	recent := recentTurnSummaries(roomID, 8)
	nextAgent, reason, err := pickNextSpeaker(
		client,
		u.Str(room["topic"]),
		u.Str(room["agent_roles_context"]),
		recent,
	)
	if err != nil {
		return nil, err
	}
	question := fmt.Sprintf("[AI 仲裁] %s。请就此发表意见。", reason)
	turn, err := store.CreateArbitrationTurn(roomID, nextAgent, question)
	if err != nil {
		return nil, err
	}
	return &ArbitrateResult{NextAgent: nextAgent, Reason: reason, Turn: turn}, nil
}

func recentTurnSummaries(roomID string, limit int) []TurnSummary {
	turns, _ := store.ListMeetingTurns(roomID)
	start := 0
	if len(turns) > limit {
		start = len(turns) - limit
	}
	var recent []TurnSummary
	for i := start; i < len(turns); i++ {
		t := turns[i]
		txt := u.Str(t["question"])
		if r := u.Str(t["response"]); r != "" {
			txt = r
		}
		recent = append(recent, TurnSummary{
			SpeakerType: u.Str(t["speaker_type"]),
			SpeakerID:   u.Str(t["speaker_id"]),
			Content:     txt,
			AddressTo:   u.Str(t["address_to"]),
		})
	}
	return recent
}
