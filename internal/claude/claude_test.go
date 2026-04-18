package claude

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"media-server-pro/internal/config"
)

// ---------------------------------------------------------------------------
// redact / redactMap
// ---------------------------------------------------------------------------

func TestRedact_Empty(t *testing.T) {
	if got := redact(""); got != "" {
		t.Errorf("redact(\"\") = %q, want \"\"", got)
	}
}

func TestRedact_BearerToken(t *testing.T) {
	in := "Authorization: Bearer abcdef0123456789ABCDEF"
	out := redact(in)
	if strings.Contains(out, "abcdef0123456789ABCDEF") {
		t.Errorf("bearer token leaked: %q", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("expected [REDACTED] marker, got %q", out)
	}
}

func TestRedact_APIKeyAssignment(t *testing.T) {
	in := `api_key = "supersecretvalue12345"`
	out := redact(in)
	if strings.Contains(out, "supersecretvalue12345") {
		t.Errorf("api_key value leaked: %q", out)
	}
}

func TestRedact_MySQLDSNPassword(t *testing.T) {
	in := "myuser:mypass123@tcp(db:3306)/app"
	out := redact(in)
	if strings.Contains(out, "mypass123") {
		t.Errorf("mysql password leaked: %q", out)
	}
	if !strings.Contains(out, "myuser:[REDACTED]@tcp(") {
		t.Errorf("expected masked dsn, got %q", out)
	}
}

func TestRedact_JSONPasswordField(t *testing.T) {
	in := `{"password":"hunter2","user":"alice"}`
	out := redact(in)
	if strings.Contains(out, "hunter2") {
		t.Errorf("json password leaked: %q", out)
	}
	if !strings.Contains(out, "alice") {
		t.Errorf("unrelated field was scrubbed: %q", out)
	}
}

func TestRedact_AnthropicKey(t *testing.T) {
	in := "key=sk-ant-api03-abcDEF-0123456789012345678901"
	out := redact(in)
	if strings.Contains(out, "sk-ant-api03-abcDEF") {
		t.Errorf("anthropic key leaked: %q", out)
	}
	if !strings.Contains(out, "[REDACTED_API_KEY]") {
		t.Errorf("expected api key marker, got %q", out)
	}
}

func TestRedact_NoMatchesUnchanged(t *testing.T) {
	in := "plain log line with no secrets"
	if out := redact(in); out != in {
		t.Errorf("redact mutated clean input: %q -> %q", in, out)
	}
}

func TestRedactMap_Nil(t *testing.T) {
	if out := redactMap(nil); out != nil {
		t.Errorf("redactMap(nil) = %v, want nil", out)
	}
}

func TestRedactMap_SensitiveKeys(t *testing.T) {
	in := map[string]any{
		"api_key":  "verysecret",
		"password": "hunter2",
		"username": "alice",
	}
	out := redactMap(in)
	if out["api_key"] != "[REDACTED]" {
		t.Errorf("api_key = %v, want [REDACTED]", out["api_key"])
	}
	if out["password"] != "[REDACTED]" {
		t.Errorf("password = %v, want [REDACTED]", out["password"])
	}
	if out["username"] != "alice" {
		t.Errorf("username = %v, want alice", out["username"])
	}
}

func TestRedactMap_EmptyStringNotRedacted(t *testing.T) {
	in := map[string]any{"password": ""}
	out := redactMap(in)
	if out["password"] != "" {
		t.Errorf("empty password was redacted: %v", out["password"])
	}
}

func TestRedactMap_NestedMap(t *testing.T) {
	in := map[string]any{
		"outer": map[string]any{
			"secret": "abcdef",
			"public": "hello",
		},
	}
	out := redactMap(in)
	nested, ok := out["outer"].(map[string]any)
	if !ok {
		t.Fatalf("nested map missing: %#v", out["outer"])
	}
	if nested["secret"] != "[REDACTED]" {
		t.Errorf("nested secret = %v", nested["secret"])
	}
	if nested["public"] != "hello" {
		t.Errorf("nested public = %v", nested["public"])
	}
}

func TestRedactMap_StringValueRedacted(t *testing.T) {
	in := map[string]any{
		"note": "connect with myuser:mypass@tcp(db)",
	}
	out := redactMap(in)
	if s, ok := out["note"].(string); !ok || strings.Contains(s, "mypass") {
		t.Errorf("note not redacted: %v", out["note"])
	}
}

// ---------------------------------------------------------------------------
// selectMode
// ---------------------------------------------------------------------------

func TestSelectMode_ValidOverride(t *testing.T) {
	if got := selectMode(ModeAutonomous, "advisory"); got != ModeAdvisory {
		t.Errorf("selectMode(autonomous, advisory) = %q, want advisory", got)
	}
	if got := selectMode(ModeAdvisory, "interactive"); got != ModeInteractive {
		t.Errorf("selectMode(advisory, interactive) = %q, want interactive", got)
	}
}

func TestSelectMode_InvalidOverrideFallsBackToCfg(t *testing.T) {
	if got := selectMode(ModeAdvisory, "garbage"); got != ModeAdvisory {
		t.Errorf("invalid override not ignored: %q", got)
	}
}

func TestSelectMode_EmptyOverrideUsesCfg(t *testing.T) {
	if got := selectMode(ModeInteractive, ""); got != ModeInteractive {
		t.Errorf("selectMode(interactive, \"\") = %q", got)
	}
}

func TestSelectMode_InvalidCfgDefaultsToAutonomous(t *testing.T) {
	if got := selectMode("bogus", ""); got != ModeAutonomous {
		t.Errorf("invalid cfg should fall back to autonomous, got %q", got)
	}
}

func TestSelectMode_CaseAndWhitespaceNormalized(t *testing.T) {
	if got := selectMode("", "  ADVISORY  "); got != ModeAdvisory {
		t.Errorf("case/whitespace not normalized: %q", got)
	}
}

// ---------------------------------------------------------------------------
// toInt
// ---------------------------------------------------------------------------

func TestToInt_Int(t *testing.T) {
	n, ok := toInt(42)
	if !ok || n != 42 {
		t.Errorf("toInt(42) = (%d, %v)", n, ok)
	}
}

func TestToInt_Int64(t *testing.T) {
	n, ok := toInt(int64(7))
	if !ok || n != 7 {
		t.Errorf("toInt(int64) = (%d, %v)", n, ok)
	}
}

func TestToInt_Float64(t *testing.T) {
	n, ok := toInt(float64(9))
	if !ok || n != 9 {
		t.Errorf("toInt(float64) = (%d, %v)", n, ok)
	}
}

func TestToInt_StringNumeric(t *testing.T) {
	n, ok := toInt("123")
	if !ok || n != 123 {
		t.Errorf("toInt(\"123\") = (%d, %v)", n, ok)
	}
}

func TestToInt_StringInvalid(t *testing.T) {
	if _, ok := toInt("abc"); ok {
		t.Errorf("toInt(\"abc\") should not be ok")
	}
}

func TestToInt_UnsupportedType(t *testing.T) {
	if _, ok := toInt([]int{1}); ok {
		t.Errorf("toInt(slice) should not be ok")
	}
}

// ---------------------------------------------------------------------------
// summarize
// ---------------------------------------------------------------------------

func TestSummarize_EmptyReturnsPlaceholder(t *testing.T) {
	if got := summarize("", 80); got != "New conversation" {
		t.Errorf("summarize(\"\") = %q", got)
	}
}

func TestSummarize_WhitespaceReturnsPlaceholder(t *testing.T) {
	if got := summarize("   \n  ", 80); got != "New conversation" {
		t.Errorf("summarize whitespace = %q", got)
	}
}

func TestSummarize_ShortUnchanged(t *testing.T) {
	if got := summarize("hi there", 80); got != "hi there" {
		t.Errorf("summarize(\"hi there\") = %q", got)
	}
}

func TestSummarize_TruncatesAndAppendsEllipsis(t *testing.T) {
	in := strings.Repeat("a", 100)
	got := summarize(in, 10)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected ellipsis suffix, got %q", got)
	}
	// 10 'a's + the ellipsis rune.
	if !strings.HasPrefix(got, strings.Repeat("a", 10)) {
		t.Errorf("unexpected prefix: %q", got)
	}
}

func TestSummarize_CollapsesNewlines(t *testing.T) {
	if got := summarize("line1\nline2", 80); strings.Contains(got, "\n") {
		t.Errorf("newline survived: %q", got)
	}
}

// ---------------------------------------------------------------------------
// permissionModeFor
// ---------------------------------------------------------------------------

func TestPermissionModeFor_Advisory(t *testing.T) {
	if got := permissionModeFor(ModeAdvisory); got != "plan" {
		t.Errorf("advisory -> %q, want plan", got)
	}
}

func TestPermissionModeFor_Interactive(t *testing.T) {
	if got := permissionModeFor(ModeInteractive); got != "bypassPermissions" {
		t.Errorf("interactive -> %q", got)
	}
}

func TestPermissionModeFor_Autonomous(t *testing.T) {
	if got := permissionModeFor(ModeAutonomous); got != "bypassPermissions" {
		t.Errorf("autonomous -> %q", got)
	}
}

func TestPermissionModeFor_UnknownDefaults(t *testing.T) {
	if got := permissionModeFor("bogus"); got != "bypassPermissions" {
		t.Errorf("unknown mode -> %q", got)
	}
}

func TestPermissionModeFor_CaseInsensitive(t *testing.T) {
	if got := permissionModeFor("  ADVISORY "); got != "plan" {
		t.Errorf("case/whitespace not normalized: %q", got)
	}
}

// ---------------------------------------------------------------------------
// unwrapToolResultContent
// ---------------------------------------------------------------------------

func TestUnwrapToolResultContent_Empty(t *testing.T) {
	if got := unwrapToolResultContent(nil); got != "" {
		t.Errorf("nil -> %q", got)
	}
}

func TestUnwrapToolResultContent_String(t *testing.T) {
	raw := json.RawMessage(`"hello"`)
	if got := unwrapToolResultContent(raw); got != "hello" {
		t.Errorf("string -> %q", got)
	}
}

func TestUnwrapToolResultContent_BlockArray(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":"foo"},{"type":"text","text":"bar"}]`)
	if got := unwrapToolResultContent(raw); got != "foobar" {
		t.Errorf("array -> %q", got)
	}
}

func TestUnwrapToolResultContent_UnknownFallsBackToRaw(t *testing.T) {
	raw := json.RawMessage(`{"unexpected":"shape"}`)
	got := unwrapToolResultContent(raw)
	if got != string(raw) {
		t.Errorf("fallback mismatch: %q", got)
	}
}

// ---------------------------------------------------------------------------
// parseCLIStream / dispatchCLIEvent
// ---------------------------------------------------------------------------

func TestParseCLIStream_SessionIDAndText(t *testing.T) {
	stream := `
{"type":"system","subtype":"init","session_id":"sess-abc"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello "}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"world"}],"stop_reason":"end_turn"}}
`
	var (
		result    cliRunResult
		finalText strings.Builder
		events    []Event
	)
	err := parseCLIStream(strings.NewReader(stream), &result, &finalText, func(ev Event) {
		events = append(events, ev)
	})
	if err != nil {
		t.Fatalf("parseCLIStream error: %v", err)
	}
	if result.SessionID != "sess-abc" {
		t.Errorf("SessionID = %q", result.SessionID)
	}
	if result.StopReason != "end_turn" {
		t.Errorf("StopReason = %q", result.StopReason)
	}
	if finalText.String() != "Hello world" {
		t.Errorf("finalText = %q", finalText.String())
	}
	// Two delta events expected.
	var deltas int
	for _, ev := range events {
		if ev.Type == "delta" {
			deltas++
		}
	}
	if deltas != 2 {
		t.Errorf("delta count = %d, want 2", deltas)
	}
}

func TestParseCLIStream_ToleratesMalformed(t *testing.T) {
	stream := "{not json}\n{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"ok\"}\n"
	var (
		result    cliRunResult
		finalText strings.Builder
	)
	err := parseCLIStream(strings.NewReader(stream), &result, &finalText, func(Event) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SessionID != "ok" {
		t.Errorf("parser should keep going past malformed lines; SessionID=%q", result.SessionID)
	}
}

func TestDispatchCLIEvent_ToolUseAndResult(t *testing.T) {
	var events []Event
	emit := func(ev Event) { events = append(events, ev) }
	var (
		result    cliRunResult
		finalText strings.Builder
	)

	dispatchCLIEvent(cliEvent{
		Type: "assistant",
		Message: &cliMessage{
			Role: "assistant",
			Content: []cliContentBlock{
				{Type: "tool_use", ID: "tu-1", Name: "Read", Input: json.RawMessage(`{"path":"a"}`)},
			},
		},
	}, &result, &finalText, emit)

	dispatchCLIEvent(cliEvent{
		Type: "user",
		Message: &cliMessage{
			Role: "user",
			Content: []cliContentBlock{
				{Type: "tool_result", ToolUseID: "tu-1", Content: json.RawMessage(`"done"`)},
			},
		},
	}, &result, &finalText, emit)

	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
	if events[0].Type != "tool_call" || events[0].ToolCall == nil || events[0].ToolCall.ID != "tu-1" {
		t.Errorf("tool_call event wrong: %+v", events[0])
	}
	if events[1].Type != "tool_result" || events[1].ToolCall == nil || events[1].ToolCall.Output != "done" {
		t.Errorf("tool_result event wrong: %+v", events[1])
	}
}

func TestDispatchCLIEvent_ResultError(t *testing.T) {
	var events []Event
	var (
		result    cliRunResult
		finalText strings.Builder
	)
	dispatchCLIEvent(cliEvent{
		Type:    "result",
		Subtype: "error_max_turns",
		IsError: true,
		Error:   json.RawMessage(`"too many turns"`),
	}, &result, &finalText, func(ev Event) { events = append(events, ev) })

	if result.StopReason != "error_max_turns" {
		t.Errorf("StopReason = %q", result.StopReason)
	}
	if len(events) != 1 || events[0].Type != "error" {
		t.Errorf("expected one error event, got %+v", events)
	}
}

// ---------------------------------------------------------------------------
// rateLimiter
// ---------------------------------------------------------------------------

func TestRateLimiter_ZeroLimitDisablesCheck(t *testing.T) {
	r := newRateLimiter()
	for i := 0; i < 1000; i++ {
		if !r.allow("u", 0) {
			t.Fatalf("zero limit should always allow (i=%d)", i)
		}
	}
}

func TestRateLimiter_UnderLimitAllowed(t *testing.T) {
	r := newRateLimiter()
	for i := 0; i < 5; i++ {
		if !r.allow("u", 5) {
			t.Errorf("call %d under limit was denied", i)
		}
	}
}

func TestRateLimiter_OverLimitDenied(t *testing.T) {
	r := newRateLimiter()
	for i := 0; i < 3; i++ {
		if !r.allow("u", 3) {
			t.Fatalf("call %d should be allowed", i)
		}
	}
	if r.allow("u", 3) {
		t.Errorf("4th call should be denied")
	}
}

func TestRateLimiter_PerUserIsolation(t *testing.T) {
	r := newRateLimiter()
	for i := 0; i < 2; i++ {
		r.allow("alice", 2)
	}
	if !r.allow("bob", 2) {
		t.Errorf("bob should have his own budget")
	}
}

func TestRateLimiter_OldEntriesExpire(t *testing.T) {
	r := newRateLimiter()
	// Seed with timestamps well outside the 1-minute sliding window.
	r.buckets["u"] = []time.Time{
		time.Now().Add(-2 * time.Minute),
		time.Now().Add(-90 * time.Second),
	}
	if !r.allow("u", 2) {
		t.Errorf("stale entries should have been pruned")
	}
}

// ---------------------------------------------------------------------------
// buildSystemPrompt
// ---------------------------------------------------------------------------

func TestBuildSystemPrompt_IncludesModeAndHost(t *testing.T) {
	m := &Module{}
	out := m.buildSystemPrompt(config.ClaudeConfig{}, ModeAutonomous)
	if !strings.Contains(out, "Operational mode: autonomous") {
		t.Errorf("mode header missing: %q", out)
	}
	if !strings.Contains(out, "host ") {
		t.Errorf("host identity missing: %q", out)
	}
}

func TestBuildSystemPrompt_AdvisoryDescription(t *testing.T) {
	m := &Module{}
	out := m.buildSystemPrompt(config.ClaudeConfig{}, ModeAdvisory)
	if !strings.Contains(strings.ToLower(out), "advisory") {
		t.Errorf("advisory description missing: %q", out)
	}
}

func TestBuildSystemPrompt_AppendsOperatorNotes(t *testing.T) {
	m := &Module{}
	cfg := config.ClaudeConfig{SystemPrompt: "staging only; skip scans"}
	out := m.buildSystemPrompt(cfg, ModeAutonomous)
	if !strings.Contains(out, "Operator notes") {
		t.Errorf("operator notes section missing: %q", out)
	}
	if !strings.Contains(out, "staging only; skip scans") {
		t.Errorf("operator text missing: %q", out)
	}
}

func TestBuildSystemPrompt_OmitsEmptyOperatorNotes(t *testing.T) {
	m := &Module{}
	out := m.buildSystemPrompt(config.ClaudeConfig{}, ModeAutonomous)
	if strings.Contains(out, "Operator notes") {
		t.Errorf("empty operator notes should be omitted: %q", out)
	}
}

// ---------------------------------------------------------------------------
// hostIdentity
// ---------------------------------------------------------------------------

func TestHostIdentity_NonEmpty(t *testing.T) {
	if got := hostIdentity(); got == "" {
		t.Errorf("hostIdentity() must never return empty")
	}
}

// ---------------------------------------------------------------------------
// writeSSE
// ---------------------------------------------------------------------------

type fakeFlusher struct{ calls int }

func (f *fakeFlusher) Flush() { f.calls++ }

func TestWriteSSE_FormatsAndFlushes(t *testing.T) {
	var buf strings.Builder
	f := &fakeFlusher{}
	ok := writeSSE(&buf, f, Event{Type: "delta", Text: "hi"})
	if !ok {
		t.Fatalf("writeSSE returned false")
	}
	if f.calls != 1 {
		t.Errorf("expected 1 flush, got %d", f.calls)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "data: ") {
		t.Errorf("missing SSE data prefix: %q", out)
	}
	if !strings.HasSuffix(out, "\n\n") {
		t.Errorf("missing SSE terminator: %q", out)
	}
	// Body must be valid JSON.
	body := strings.TrimSuffix(strings.TrimPrefix(out, "data: "), "\n\n")
	var ev Event
	if err := json.Unmarshal([]byte(body), &ev); err != nil {
		t.Errorf("SSE body not valid JSON: %v", err)
	}
	if ev.Type != "delta" || ev.Text != "hi" {
		t.Errorf("decoded event wrong: %+v", ev)
	}
}

func TestWriteSSE_NilFlusherOK(t *testing.T) {
	var buf strings.Builder
	if !writeSSE(&buf, nil, Event{Type: "info"}) {
		t.Errorf("writeSSE should tolerate nil flusher")
	}
}
