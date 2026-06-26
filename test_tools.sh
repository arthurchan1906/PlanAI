#!/bin/bash
# test_tools.sh — verify aipmc proxy protocol translation
# Usage: ./test_tools.sh [proxy_url]
# Default proxy: http://localhost:19530

set -e
PROXY="${1:-http://localhost:19530}"
MODEL="${2:-qwen}"
PASS=0
FAIL=0

green() { printf "\033[32m%s\033[0m\n" "$1"; }
red()   { printf "\033[31m%s\033[0m\n" "$1"; }

check() {
    local label="$1" expected="$2" actual="$3"
    if echo "$actual" | grep -q "$expected"; then
        green "  ✅ $label"
        PASS=$((PASS + 1))
    else
        red "  ❌ $label (expected: $expected)"
        echo "     got: $actual"
        FAIL=$((FAIL + 1))
    fi
}

echo "=========================================="
echo " aipmc proxy test suite"
echo " proxy: $PROXY  model: $MODEL"
echo "=========================================="
echo ""

# ── 1. Gemini non-streaming ──────────────────────────────────
echo "── Gemini :generateContent (non-streaming) ──"

# 1a. Basic chat
RESP=$(curl -s -X POST "$PROXY/models/$MODEL:generateContent" \
  -H "Content-Type: application/json" \
  -d '{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}')
check "basic chat" "candidates" "$RESP"

# 1b. Tool calling
RESP=$(curl -s -X POST "$PROXY/models/$MODEL:generateContent" \
  -H "Content-Type: application/json" \
  -d '{
    "contents":[{"role":"user","parts":[{"text":"list files"}]}],
    "tools":[{"functionDeclarations":[{"name":"shell","description":"Run command","parametersJsonSchema":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}}]}]
  }')
check "tool calling" "functionCall" "$RESP"
check "tool: shell name" "shell" "$RESP"

# 1c. Tool round-trip
RESP=$(curl -s -X POST "$PROXY/models/$MODEL:generateContent" \
  -H "Content-Type: application/json" \
  -d '{
    "contents":[
      {"role":"user","parts":[{"text":"list files"}]},
      {"role":"model","parts":[{"functionCall":{"id":"call_x","name":"shell","args":{"command":"ls"}}}]},
      {"role":"user","parts":[{"functionResponse":{"id":"call_x","name":"shell","response":{"stdout":"README.md main.go"}}}]}
    ],
    "tools":[{"functionDeclarations":[{"name":"shell","parametersJsonSchema":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}}]}]
  }')
check "tool round-trip" "README" "$RESP"

# 1d. Multi-turn
RESP=$(curl -s -X POST "$PROXY/models/$MODEL:generateContent" \
  -H "Content-Type: application/json" \
  -d '{
    "contents":[
      {"role":"user","parts":[{"text":"my name is Bob"}]},
      {"role":"model","parts":[{"text":"Nice to meet you Bob!"}]},
      {"role":"user","parts":[{"text":"what is my name?"}]}
    ]
  }')
check "multi-turn memory" "Bob" "$RESP"

# ── 2. Gemini streaming ──────────────────────────────────────
echo ""
echo "── Gemini :streamGenerateContent ──"

STREAM=$(curl -s -N -X POST "$PROXY/models/$MODEL:streamGenerateContent" \
  -H "Content-Type: application/json" \
  -d '{"contents":[{"role":"user","parts":[{"text":"say hello in one word"}]}]}')
check "streaming response" "data:" "$STREAM"
check "stream has content" "Hello\|hello\|Hi" "$STREAM"

# ── 3. Count tokens ──────────────────────────────────────────
echo ""
echo "── Gemini :countTokens ──"

TOKENS=$(curl -s -X POST "$PROXY/models/$MODEL:countTokens" \
  -H "Content-Type: application/json" \
  -d '{"contents":[{"role":"user","parts":[{"text":"hello world"}]}]}')
check "countTokens" "totalTokens" "$TOKENS"

# ── 4. Anthropic Messages ────────────────────────────────────
echo ""
echo "── Anthropic /v1/messages ──"

ANT=$(curl -s -X POST "$PROXY/v1/messages" \
  -H "Content-Type: application/json" \
  -H "x-api-key: test" \
  -d '{"model":"claude","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}')
check "anthropic basic" '"type":"message"' "$ANT"

ANT_STREAM=$(curl -s -N -X POST "$PROXY/v1/messages" \
  -H "Content-Type: application/json" \
  -H "x-api-key: test" \
  -d '{"model":"claude","max_tokens":50,"messages":[{"role":"user","content":"hi"}],"stream":true}')
check "anthropic streaming" "message_start" "$ANT_STREAM"

# ── 5. Responses API (Codex) ─────────────────────────────────
echo ""
echo "── Responses /v1/responses ──"

CODE=$(curl -s -X POST "$PROXY/v1/responses" \
  -H "Content-Type: application/json" \
  -d '{"model":"qwen","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}],"max_output_tokens":500}')
check "responses basic" '"status":"completed"' "$CODE"

CODE_TOOL=$(curl -s -X POST "$PROXY/v1/responses" \
  -H "Content-Type: application/json" \
  -d '{
    "model":"qwen","stream":false,
    "input":[
      {"type":"message","role":"user","content":[{"type":"input_text","text":"list files"}]},
      {"type":"function_call","call_id":"c1","name":"shell","arguments":"{\"command\":\"ls\"}"},
      {"type":"function_call_output","call_id":"c1","output":"README.md go.mod main.go"}
    ],
    "tools":[{"type":"function","name":"shell","parameters":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}}],
    "max_output_tokens":500
  }')
check "responses tool round-trip" "README\|main" "$CODE_TOOL"

# ── 6. Passthrough (OpenAI / Cursor) ─────────────────────────
echo ""
echo "── Passthrough /v1/models ──"

MODELS=$(curl -s "$PROXY/v1/models")
check "models passthrough" '"object":"list"' "$MODELS"

# ── Summary ──────────────────────────────────────────────────
echo ""
echo "=========================================="
printf " Results: %d passed, %d failed\n" $PASS $FAIL
if [ $FAIL -eq 0 ]; then
    green " All tests passed! 🎉"
else
    red " Some tests failed. Check output above."
    exit 1
fi
echo "=========================================="
