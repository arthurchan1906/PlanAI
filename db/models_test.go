package db

import "testing"

func TestRealModelForRouteResponses(t *testing.T) {
	rt := &ModelRoute{Provider: "deepseek", ModelOpenAI: "deepseek-chat", ModelAnthropic: "deepseek-claude", ModelResponses: "deepseek-v4-flash"}
	if got := realModelForRoute(rt, "responses"); got != "deepseek-v4-flash" {
		t.Fatalf("responses protocol: got %q want %q", got, "deepseek-v4-flash")
	}
	if got := realModelForRoute(rt, "openai"); got != "deepseek-chat" {
		t.Fatalf("openai protocol: got %q want %q", got, "deepseek-chat")
	}
}

func TestModelForAgentProtoCodexResponses(t *testing.T) {
	reg := &ModelRegistry{Models: []VirtualModel{{
		ID: "deepseek-v4-pro",
		Routes: []ModelRoute{{
			Provider:       "deepseek",
			ModelOpenAI:    "deepseek-chat",
			ModelResponses: "deepseek-v4-pro",
		}},
	}}}
	if got := reg.ModelForAgentProto("deepseek-v4-pro", "codex"); got != "deepseek-v4-pro" {
		t.Fatalf("codex proto: got %q want %q", got, "deepseek-v4-pro")
	}
}

func TestModelForAgentProtoCodexFallsBackToOpenAI(t *testing.T) {
	reg := &ModelRegistry{Models: []VirtualModel{{
		ID: "deepseek-v4-pro",
		Routes: []ModelRoute{{
			Provider:    "deepseek",
			ModelOpenAI: "deepseek-chat",
		}},
	}}}
	if got := reg.ModelForAgentProto("deepseek-v4-pro", "codex"); got != "deepseek-chat" {
		t.Fatalf("codex fallback: got %q want %q", got, "deepseek-chat")
	}
}
