package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// =============================================================================
// Request/response capture 鈥?in-memory ring buffer for web inspector
// =============================================================================

// captureEntry holds a single captured proxy exchange (request + response).
// Data lives only in memory; never persisted to database.
type captureEntry struct {
	ID        string `json:"id"`
	Time      string `json:"time"`
	Agent     string `json:"agent"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	Model     string `json:"model"`
	Status    int    `json:"status"`
	Duration  string `json:"duration"`
	ReqSize   int    `json:"req_size"`
	RespSize  int    `json:"resp_size"`

	// Full request
	ReqHeaders map[string]string `json:"req_headers"`
	ReqBody    string            `json:"req_body"`
	ReqUnified string            `json:"req_unified,omitempty"` // UnifiedReq JSON

	// Full response
	RespHeaders map[string]string `json:"resp_headers,omitempty"`
	RespBody    string            `json:"resp_body"`     // raw response text
	RespEvents  string            `json:"resp_events,omitempty"` // SSE events as JSON array

	// Token usage (populated after response completes)
	PromptTokens      int `json:"prompt_tokens"`
	CompletionTokens  int `json:"completion_tokens"`
	CacheHitTokens    int `json:"cache_hit_tokens"`
	CacheCreationTokens int `json:"cache_creation_tokens"`
}

var (
	captureMu   sync.Mutex
	captureLog  []captureEntry
	maxCapture  = 200
	captureSeq  int
)

// startCapture begins a new capture entry, returning its ID.
// Call finishCapture when the response completes.
func startCapture(agent, method, path, model string, reqBody []byte, reqHeaders map[string]string, unifiedReq *UnifiedReq) string {
	captureMu.Lock()
	defer captureMu.Unlock()

	captureSeq++
	id := fmt.Sprintf("cap_%d", captureSeq)

	reqUnified := ""
	if unifiedReq != nil {
		b, _ := json.Marshal(unifiedReq)
		reqUnified = string(b)
	}

	entry := captureEntry{
		ID:          id,
		Time:        time.Now().Format("15:04:05.000"),
		Agent:       agent,
		Method:      method,
		Path:        path,
		Model:       model,
		ReqSize:     len(reqBody),
		ReqHeaders:  reqHeaders,
		ReqBody:     string(reqBody),
		ReqUnified:  reqUnified,
	}
	captureLog = append(captureLog, entry)
	if len(captureLog) > maxCapture {
		captureLog = captureLog[len(captureLog)-maxCapture:]
	}
	return id
}

// SetCaptureTokens sets token usage on an existing capture entry.
func SetCaptureTokens(id string, promptTokens, completionTokens int) {
	captureMu.Lock()
	defer captureMu.Unlock()
	for i := range captureLog {
		if captureLog[i].ID == id {
			captureLog[i].PromptTokens = promptTokens
			captureLog[i].CompletionTokens = completionTokens
			return
		}
	}
}

// SetCaptureCacheTokens sets cache token usage on an existing capture entry.
func SetCaptureCacheTokens(id string, cacheHitTokens, cacheCreationTokens int) {
	captureMu.Lock()
	defer captureMu.Unlock()
	for i := range captureLog {
		if captureLog[i].ID == id {
			captureLog[i].CacheHitTokens = cacheHitTokens
			captureLog[i].CacheCreationTokens = cacheCreationTokens
			return
		}
	}
}

// finishCapture completes a capture entry with response data.
func finishCapture(id string, status int, duration time.Duration, respHeaders map[string]string, respBody string, respEvents string) {
	captureMu.Lock()
	defer captureMu.Unlock()

	for i := range captureLog {
		if captureLog[i].ID == id {
			captureLog[i].Status = status
			captureLog[i].Duration = duration.Truncate(time.Millisecond).String()
			captureLog[i].RespSize = len(respBody)
			captureLog[i].RespHeaders = respHeaders
			captureLog[i].RespBody = respBody
			captureLog[i].RespEvents = respEvents
			return
		}
	}
}

// =============================================================================
// HTTP handlers
// =============================================================================

// handleCaptureList returns the list of captured exchanges (summary only,
// without full request/response bodies). If ?id=xxx is provided, returns
// the full detail for that capture instead.
func handleCaptureList(w http.ResponseWriter, r *http.Request) {
	if id := r.URL.Query().Get("id"); id != "" {
		handleCaptureDetail(w, r)
		return
	}
	captureMu.Lock()
	entries := make([]captureEntry, len(captureLog))
	copy(entries, captureLog)
	captureMu.Unlock()

	sort.Slice(entries, func(i, j int) bool { return entries[i].Time > entries[j].Time })

	// Pagination
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage < 1 || perPage > 200 {
		perPage = 50
	}
	total := len(entries)
	totalPages := (total + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}
	start := (page - 1) * perPage
	end := start + perPage
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}

	type summary struct {
		ID               string `json:"id"`
		Time             string `json:"time"`
		Agent            string `json:"agent"`
		Method           string `json:"method"`
		Path             string `json:"path"`
		Model            string `json:"model"`
		Status           int    `json:"status"`
		Duration         string `json:"duration"`
		ReqSize          int    `json:"req_size"`
		RespSize         int    `json:"resp_size"`
		PromptTokens     int    `json:"prompt_tokens"`
		CompletionTokens int    `json:"completion_tokens"`
		CacheHitTokens   int    `json:"cache_hit_tokens"`
	}
	var list []summary
	for _, e := range entries[start:end] {
		list = append(list, summary{
			ID: e.ID, Time: e.Time, Agent: e.Agent, Method: e.Method,
			Path: e.Path, Model: e.Model, Status: e.Status,
			Duration: e.Duration, ReqSize: e.ReqSize, RespSize: e.RespSize,
			PromptTokens: e.PromptTokens, CompletionTokens: e.CompletionTokens,
			CacheHitTokens: e.CacheHitTokens,
		})
	}
	if list == nil {
		list = []summary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"captures":    list,
		"total":       total,
		"page":        page,
		"per_page":    perPage,
		"total_pages": totalPages,
	})
}

// handleCaptureDetail returns the full request and response for a single capture.
func handleCaptureDetail(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	captureMu.Lock()
	var entry *captureEntry
	for i := range captureLog {
		if captureLog[i].ID == id {
			entry = &captureLog[i]
			break
		}
	}
	captureMu.Unlock()

	if entry == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, entry)
}

// handleCaptureClear clears all captured entries.
func handleCaptureClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != "DELETE" && r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	captureMu.Lock()
	captureLog = nil
	captureMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleInspectPage serves the inspector HTML page.
func handleInspectPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(inspectHTML))
}

// =============================================================================
// Capture helpers for stream/non-stream handlers
// =============================================================================

// captureStreamResponse reads SSE events from a channel, accumulates them as
// JSON for the capture log, and returns the full response text.
type streamCapture struct {
	Events  []UnifiedStreamEvent
	RawJSON strings.Builder
}

func newStreamCapture() *streamCapture {
	return &streamCapture{}
}

func (c *streamCapture) addEvent(event UnifiedStreamEvent) {
	c.Events = append(c.Events, event)
	b, _ := json.Marshal(event)
	if c.RawJSON.Len() > 0 {
		c.RawJSON.WriteString(",\n")
	}
	c.RawJSON.Write(b)
}

func (c *streamCapture) eventsJSON() string {
	return "[" + c.RawJSON.String() + "]"
}

func (c *streamCapture) responseText() string {
	var sb strings.Builder
	for _, ev := range c.Events {
		if ev.Type == StreamText {
			sb.WriteString(ev.Delta)
		}
	}
	return sb.String()
}

// copyHeaders copies relevant request headers for capture.
func copyHeaders(r *http.Request) map[string]string {
	h := make(map[string]string)
	for k, vs := range r.Header {
		if k == "Authorization" {
			h[k] = "Bearer ..."
			continue
		}
		h[k] = strings.Join(vs, ", ")
	}
	return h
}

// readBody reads and returns the request body, replacing r.Body with a new reader.
func readBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	return body, nil
}

const inspectHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Proxy Inspector</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:system-ui,monospace;background:#0d1117;color:#c9d1d9;padding:16px}
h1{font-size:18px;margin-bottom:12px;color:#58a6ff}
table{width:100%;border-collapse:collapse;font-size:13px}
th,td{padding:6px 10px;text-align:left;border-bottom:1px solid #21262d}
th{background:#161b22}
tr:hover{background:#161b22}
.s-ok{color:#3fb950}.s-err{color:#f85149}
.a-gemini{color:#58a6ff}.a-codex{color:#3fb950}.a-claude{color:#f0883e}.a-cursor{color:#bc8cff}
.size{text-align:right;color:#8b949e}
.btn{padding:4px 12px;border:1px solid #30363d;border-radius:4px;cursor:pointer;font-size:12px;background:#21262d;color:#c9d1d9;margin-right:8px}
.btn:hover{background:#30363d}.btn-danger{color:#f85149;border-color:#f85149}
#drawer{position:fixed;top:0;right:0;width:780px;height:100vh;background:#161b22;border-left:1px solid #30363d;z-index:100;transform:translateX(100%);transition:transform .25s;overflow-y:auto;padding:20px}
#drawer.open{transform:translateX(0)}
#mask{position:fixed;top:0;left:0;width:100%;height:100%;background:rgba(0,0,0,.5);z-index:99;display:none}
#mask.on{display:block}
#drawer h2{font-size:14px;margin-bottom:8px;color:#f0883e}
.tabs{display:flex;gap:8px;margin-bottom:12px;border-bottom:1px solid #21262d;padding-bottom:8px}
.tab{padding:4px 12px;border:1px solid #30363d;border-radius:4px;cursor:pointer;font-size:12px;background:#0d1117;color:#8b949e}
.tab.on{background:#1f6feb;color:#fff;border-color:#1f6feb}
.pane{display:none}.pane.on{display:block}
pre{background:#0d1117;border:1px solid #30363d;border-radius:4px;padding:12px;font-size:12px;white-space:pre-wrap;word-break:break-all;max-height:60vh;overflow:auto}
/* Messages tab */
.msg-item{border:1px solid #21262d;border-radius:4px;margin-bottom:4px;overflow:hidden}
.msg-header{display:flex;align-items:center;gap:8px;padding:6px 10px;cursor:pointer;font-size:12px;background:#0d1117}
.msg-header:hover{background:#161b22}
.msg-role{display:inline-block;padding:1px 6px;border-radius:3px;font-size:11px;font-weight:600;text-transform:uppercase;flex-shrink:0;min-width:60px;text-align:center}
.msg-role.user{background:#1f6feb22;color:#58a6ff;border:1px solid #1f6feb44}
.msg-role.assistant{background:#3fb95022;color:#3fb950;border:1px solid #3fb95044}
.msg-role.tool{background:#f0883e22;color:#f0883e;border:1px solid #f0883e44}
.msg-preview{color:#8b949e;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;flex:1;font-size:11px}
.msg-body{padding:10px;background:#0d1117;border-top:1px solid #21262d;font-size:12px;white-space:pre-wrap;word-break:break-word;font-family:ui-monospace,monospace;color:#c9d1d9;max-height:350px;overflow:auto;display:none}
.msg-body.open{display:block}
.msg-arrow{color:#484f58;font-size:10px;flex-shrink:0;transition:transform .15s}
.msg-arrow.open{transform:rotate(90deg)}
.msg-count{font-size:11px;color:#484f58;margin-bottom:6px}
.meta{display:flex;gap:12px;flex-wrap:wrap;margin-bottom:8px;font-size:12px;color:#8b949e}
.controls{display:flex;align-items:center;margin-bottom:12px}
.close{position:absolute;top:12px;right:16px;cursor:pointer;font-size:20px;color:#8b949e;background:none;border:none}
.close:hover{color:#f85149}
</style>
</head>
<body>
<h1>Proxy Inspector</h1>
<div class="controls">
  <button class="btn" onclick="loadList()">Refresh</button>
  <button class="btn btn-danger" onclick="clearAll()">Clear All</button>
</div>
<table><thead><tr>
  <th>Time</th><th>Agent</th><th>Method</th><th>Model</th><th>St</th><th class="size">Tokens</th><th class="size">Req</th><th class="size">Resp</th><th></th>
</tr></thead><tbody id="list"></tbody></table>
<div id="pager" style="display:flex;align-items:center;gap:6px;margin-top:10px;font-size:12px;color:#8b949e"></div>
<div id="mask" onclick="closeDrawer()"></div>
<div id="drawer">
  <button class="close" onclick="closeDrawer()">✕</button>
  <h2 id="dtitle"></h2>
  <div class="meta" id="dmeta"></div>
  <div class="tabs">
    <div class="tab on" onclick="switchTab('request')">Request</div>
    <div class="tab" onclick="switchTab('unified')">Unified</div>
    <div class="tab" onclick="switchTab('response')">Response</div>
    <div class="tab" onclick="switchTab('events')">SSE Events</div>
    <div class="tab" onclick="switchTab('messages')">Messages</div>
  </div>
  <div class="pane on" id="preq"><pre></pre></div>
  <div class="pane" id="punified"><pre></pre></div>
  <div class="pane" id="presp"><pre></pre></div>
  <div class="pane" id="pevents"><pre></pre></div>
  <div class="pane" id="pmsg"><div id="msgcnt"></div><div id="msglist"></div></div>
</div>
<script>
let auto=setInterval(loadList,3000);
function sc(s){return s<400?'s-ok':'s-err'}
function ac(a){return 'a-'+(a||'unknown')}
function fs(n){return n>1024?(n/1024).toFixed(1)+'KB':n+'B'}
function tk(p,c,h){if(!p&&!c)return'';var s=p+'/'+c;if(h){var r=p>0?(h/p*100).toFixed(2):0;return s+'<span style="color:#58a6ff;font-size:11px">·⚡'+h+' ('+r+'%)</span>'}return s}
function tf(s){if(!s)return '(empty)';try{return JSON.stringify(JSON.parse(s),null,2)}catch(e){return s}}
async function loadList(){
  const r=await fetch('/__proxy/capture');
  const d=await r.json();
  document.getElementById('list').innerHTML=(d.captures||[]).map(c=>
    '<tr><td>'+c.time+'</td><td class="'+ac(c.agent)+'">'+c.agent+'</td><td>'+c.method+'</td><td>'+c.model+'</td><td class="'+sc(c.status)+'">'+c.status+'</td><td class="size">'+tk(c.prompt_tokens,c.completion_tokens,c.cache_hit_tokens)+'</td><td class="size">'+fs(c.req_size)+'</td><td class="size">'+fs(c.resp_size)+'</td><td><button class="btn" onclick="loadDetail(\''+c.id+'\')">View</button></td></tr>'
  ).join('');
}
async function loadDetail(id){
  const r=await fetch('/__proxy/capture?id='+id);
  const c=await r.json();
  document.getElementById('dtitle').textContent=c.method+' '+c.path;
  document.getElementById('dmeta').innerHTML='<span>'+c.agent+'</span><span>'+c.time+'</span><span>Model: '+c.model+'</span><span>'+c.duration+'</span><span>Status: '+c.status+'</span>';
  document.querySelector('#preq pre').textContent=tf(c.req_body);
  document.querySelector('#punified pre').textContent=tf(c.req_unified||'(not captured)');
  document.querySelector('#presp pre').textContent=tf(c.resp_body);
  document.querySelector('#pevents pre').textContent=tf(c.resp_events||'(not captured)');
  renderMessages(c);
  switchTab('request');
  openDrawer();
}
function switchTab(n){
  document.querySelectorAll('.tab').forEach(t=>t.classList.remove('on'));
  document.querySelectorAll('.pane').forEach(p=>p.classList.remove('on'));
  document.querySelector('.tab[onclick*="'+n+'"]').classList.add('on');
  var m={'request':'preq','unified':'punified','response':'presp','events':'pevents','messages':'pmsg'};
  document.getElementById(m[n]||'preq').classList.add('on');
}
function openDrawer(){document.getElementById('drawer').classList.add('open');document.getElementById('mask').classList.add('on')}
function closeDrawer(){document.getElementById('drawer').classList.remove('open');document.getElementById('mask').classList.remove('on')}
// -- Messages tab --
// Extract normalized {role, content, toolCallID, toolCalls} from unified or raw JSON.
function extractMessages(c){
  // 1) UnifiedRequest path (Gemini/Codex/non-passthrough Claude)
  if(c.req_unified){
    try{
      var u=JSON.parse(c.req_unified);
      var umsgs=u.Messages||u.messages;if(umsgs&&Array.isArray(umsgs)) return umsgs.map(function(m,i){
        return {role:m.Role||m.role||'?',content:m.Content||m.content||'',thinking:m.Thinking||'',toolCallID:m.ToolCallID||'',toolCalls:m.ToolCalls||null,idx:i+1};
      });
    }catch(e){}
  }
  // 2) Raw body fallback — support Anthropic / OpenAI / Gemini / Codex formats
  if(!c.req_body) return [];
  try{
    var raw=JSON.parse(c.req_body);
    // Anthropic passthrough / OpenAI chat-completions: top-level "messages"
    if(raw.messages&&Array.isArray(raw.messages)){
      return raw.messages.map(function(m,i){
        var content=m.content;
        if(Array.isArray(content)){
          // Anthropic content-block array: [{type:"text",text:"..."},{type:"tool_use",...}]
          content=content.map(function(b){if(b.type==="tool_result")return b.type+"("+(b.tool_use_id||"")+"): "+JSON.stringify(b.content);if(b.type==="tool_use")return b.type+"("+(b.name||"")+"): "+JSON.stringify(b.input||"");return b.text||(b.type+"("+(b.name||b.id||"")+")");}).join('\n');
        }
        return {role:m.role||'?',content:content||'',idx:i+1};
      });
    }
    // Gemini: "contents" array
    if(raw.contents&&Array.isArray(raw.contents)){
      return raw.contents.map(function(m,i){
        var text='';
        if(m.parts&&Array.isArray(m.parts)) text=m.parts.map(function(p){return p.text||'';}).join('\n');
        return {role:m.role||'?',content:text,idx:i+1};
      });
    }
    // Codex Responses API: "input" array
    if(raw.input&&Array.isArray(raw.input)){
      return raw.input.map(function(m,i){
        var role=m.role||'?';
        var content=m.content||'';
        if(m.type==="function_call"){role='assistant';content='call: '+m.name}else if(m.type==="function_call_output"){role='tool';content='result: '+String(m.output||'').slice(0,200)}
        else if(Array.isArray(content)) content=content.map(function(b){return b.text||'';}).join('\n');
        return {role:role,content:content,idx:i+1};
      });
    }
  }catch(e){}
  return [];
}
function renderMessages(c){
  var msgs=extractMessages(c);
  var cnt=document.getElementById('msgcnt');
  var list=document.getElementById('msglist');
  if(!msgs.length){cnt.textContent='(no messages)';list.innerHTML='';return;}
  cnt.textContent=msgs.length+' messages';
  list.innerHTML=msgs.map(function(m){
    var role=m.role||'?';
    // Preview: first line of content, max 80 chars, collapse whitespace
    var preview=(m.content||'').replace(/\n/g,' ').replace(/\s+/g,' ').trim();
    if(preview.length>80) preview=preview.slice(0,80)+'…';
    if(!preview&&m.thinking) preview='[thinking '+(m.thinking.length>60?m.thinking.slice(0,60)+'…':m.thinking)+']';
    if(!preview&&m.toolCalls&&m.toolCalls.length){var tcs=m.toolCalls.map(function(t){return t.Name||'?';});preview='[call: '+tcs.join(',')+']'}
    if(!preview&&m.toolCallID) preview='[tool result: '+m.toolCallID+']';
    if(!preview) preview='(empty)';
    // Format content for display: escape HTML entities, preserve real newlines
    var body=(m.content||'').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
    if(m.thinking) body+='\n\n[thinking]\n'+m.thinking.replace(/&/g,'&amp;').replace(/</g,'&lt;');
    if(m.toolCalls&&m.toolCalls.length){body+='\n\n[tool calls]\n';m.toolCalls.forEach(function(t){body+=t.Name+'('+(t.Arguments||'')+')\n';})}
    if(m.toolCallID) body+='\n\n[tool_call_id]\n'+m.toolCallID;
    return '<div class="msg-item">'+
      '<div class="msg-header" onclick="var b=this.nextElementSibling;var a=this.querySelector(\'.msg-arrow\');b.classList.toggle(\'open\');a.classList.toggle(\'open\')">'+
        '<span class="msg-arrow">▶</span>'+
        '<span class="msg-role '+role+'">#'+m.idx+' '+role+'</span>'+
        '<span class="msg-preview">'+preview+'</span>'+
      '</div>'+
      '<div class="msg-body">'+body+'</div>'+
    '</div>';
  }).join('');
}
async function clearAll(){if(confirm('Clear?')){await fetch('/__proxy/capture/clear',{method:'POST'});loadList()}}
loadList();
</script>
</body>
</html>`


// =============================================================================
