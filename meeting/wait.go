package meeting

import (
	"fmt"
	"os"
	"time"

	"aipmc/store"
)

// WaitResult is the JSON payload from aipmc wait.
type WaitResult struct {
	Status     string `json:"status"`
	TurnID     string `json:"turn_id,omitempty"`
	RoomID     string `json:"room_id,omitempty"`
	TurnNumber int    `json:"turn_number,omitempty"`
	Question   string `json:"question,omitempty"`
	RoomTitle  string `json:"room_title,omitempty"`
}

// PollWaitingTurn checks once for a waiting turn assigned to agentID.
func PollWaitingTurn(agentID string) (*WaitResult, bool) {
	turnID, roomID, question, roomTitle, turnNum, ok := store.FindWaitingTurn(agentID)
	if !ok {
		return nil, false
	}
	return &WaitResult{
		Status:     "turn_waiting",
		TurnID:     turnID,
		RoomID:     roomID,
		TurnNumber: turnNum,
		Question:   question,
		RoomTitle:  roomTitle,
	}, true
}

// RunWaitCLI implements `aipmc wait --agent-id <id> [--timeout <seconds>]`.
func RunWaitCLI(rawArgs []string) {
	agentID := ""
	timeout := 300

	for i := 0; i < len(rawArgs); i++ {
		switch rawArgs[i] {
		case "--agent-id":
			if i+1 < len(rawArgs) {
				agentID = rawArgs[i+1]
				i++
			}
		case "--timeout":
			if i+1 < len(rawArgs) {
				fmt.Sscanf(rawArgs[i+1], "%d", &timeout)
				i++
			}
		}
	}

	if agentID == "" {
		fmt.Fprintln(os.Stderr, "Usage: aipmc wait --agent-id <id> [--timeout <seconds>]")
		os.Exit(1)
	}

	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	for {
		if time.Now().After(deadline) {
			fmt.Println(`{"status":"timeout"}`)
			return
		}
		if res, ok := PollWaitingTurn(agentID); ok {
			fmt.Printf(`{"status":"turn_waiting","turn_id":"%s","room_id":"%s","turn_number":%d,"question":"%s","room_title":"%s"}`+"\n",
				res.TurnID, res.RoomID, res.TurnNumber, res.Question, res.RoomTitle)
			return
		}
		time.Sleep(WaitPollIntervalSec * time.Second)
	}
}
