package main

import (
	"fmt"
	"os"
	"time"

	pmdb "aipmc/db"
)

// waitForTurn blocks until a pending meeting turn is found for the agent.
// Polls every 2 seconds. Exits with JSON output when a turn is waiting.
// Usage: aipmc wait --agent-id <id> [--timeout 300]
func waitForTurnCmd(rawArgs []string) {
	agentID := ""
	timeout := 300 // max wait seconds

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

		// Check if there's a pending turn for this agent
		db, err := pmdb.Open()
		if err == nil {
			rows, err := db.Query(`
				SELECT mt.id, mt.room_id, mt.turn_number, mt.question, mr.title
				FROM meeting_turns mt
				JOIN meeting_rooms mr ON mt.room_id = mr.id
				WHERE mt.speaker_id = ? AND mt.status = 'waiting' AND mr.status = 'active'
				ORDER BY mt.created_at
				LIMIT 1`, agentID)
			if err == nil {
				for rows.Next() {
					var turnID, roomID, question, roomTitle string
					var turnNum int
					rows.Scan(&turnID, &roomID, &turnNum, &question, &roomTitle)
					fmt.Printf(`{"status":"turn_waiting","turn_id":"%s","room_id":"%s","turn_number":%d,"question":"%s","room_title":"%s"}`+"\n",
						turnID, roomID, turnNum, question, roomTitle)
					rows.Close()
					db.Close()
					return
				}
				rows.Close()
			}
			db.Close()
		}

		time.Sleep(2 * time.Second)
	}
}
