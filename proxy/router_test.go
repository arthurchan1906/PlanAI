package proxy

import (
	"testing"
	pmdb "aipmc/db"
)

func TestShouldPassthroughResponses(t *testing.T) {
	reg := pmdb.ModelRegistry{
		Version: 1,
		Providers: []pmdb.Provider{{
			Name:         "deepseek",
			OpenAIURL:    "https://api.deepseek.com/v1",
			ResponsesURL: "https://api.deepseek.com/",
		}},
		Models: []pmdb.VirtualModel{{
			ID: "deepseek-v4-flash",
			Routes: []pmdb.ModelRoute{{
				Provider:       "deepseek",
				ModelOpenAI:    "deepseek-chat",
				ModelResponses: "deepseek-v4-flash",
			}},
		}},
	}
	router := &ModelRouter{registry: &reg}
	if !router.ShouldPassthroughResponses("deepseek-v4-flash") {
		t.Fatal("expected true: provider has responses_url + route has model_responses")
	}
}

func TestShouldPassthroughResponsesFalseWhenNoResponsesURL(t *testing.T) {
	reg := pmdb.ModelRegistry{
		Version: 1,
		Providers: []pmdb.Provider{{
			Name:      "deepseek",
			OpenAIURL: "https://api.deepseek.com/v1",
		}},
		Models: []pmdb.VirtualModel{{
			ID: "deepseek-v4-flash",
			Routes: []pmdb.ModelRoute{{
				Provider:       "deepseek",
				ModelOpenAI:    "deepseek-chat",
				ModelResponses: "deepseek-v4-flash",
			}},
		}},
	}
	router := &ModelRouter{registry: &reg}
	if router.ShouldPassthroughResponses("deepseek-v4-flash") {
		t.Fatal("expected false: provider has no responses_url")
	}
}

func TestShouldPassthroughResponsesFalseForUnknownModel(t *testing.T) {
	router := &ModelRouter{registry: &pmdb.ModelRegistry{Models: []pmdb.VirtualModel{{ID: "x", Routes: []pmdb.ModelRoute{{Provider: "p", ModelOpenAI: "o"}}}}}}
	if router.ShouldPassthroughResponses("nope") {
		t.Fatal("expected false: unknown model")
	}
}
