package meeting

// Room and turn status values (see docs/MEETING_DESIGN.md).
const (
	RoomActive  = "active"
	RoomClosed  = "closed"
	TurnWaiting = "waiting"
	TurnProcessing = "processing"
	TurnResponded  = "responded"
)

// Default poll interval for aipmc wait.
const WaitPollIntervalSec = 2

// DefaultArbitrationWindowSec is the delay before auto-arbitration (not yet wired).
const DefaultArbitrationWindowSec = 8

// StaleProcessingTimeoutMin resets processing turns back to waiting (TODO).
const StaleProcessingTimeoutMin = 5
