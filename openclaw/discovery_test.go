package openclaw

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	pluginbridge "github.com/Rokid-Prism/prism-plugin-sdk"
)

func TestDiscoverEndpointIgnoresStoredSessionEndpointByUsingCurrentDiscovery(t *testing.T) {
	adapter := New(Config{
		GatewayURL: "http://127.0.0.1:18789",
	})
	endpoint, detail, err := adapter.discoverEndpoint()
	if err != nil {
		t.Fatalf("discoverEndpoint: %v", err)
	}
	if endpoint != "http://127.0.0.1:18789" {
		t.Fatalf("expected configured endpoint, got %q", endpoint)
	}
	if detail == "" {
		t.Fatal("expected discovery detail")
	}
}

func TestDiscoverEndpointFallsBackWithoutUserHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
	t.Setenv("OPENCLAW_STATE_DIR", "")
	t.Setenv("OPENCLAW_HOME", "")
	t.Setenv("OPENCLAW_CONFIG_PATH", "")
	t.Setenv("OPENCLAW_GATEWAY_PORT", "")
	adapter := New(Config{})
	endpoint, _, err := adapter.discoverEndpoint()
	if err != nil || endpoint != "http://127.0.0.1:18789" {
		t.Fatalf("discoverEndpoint = (%q, %v)", endpoint, err)
	}
	if _, _, err := adapter.discoverToken(); err == nil || !strings.Contains(err.Error(), "OPENCLAW_HOME") {
		t.Fatalf("discoverToken should explain how to configure the missing home: %v", err)
	}
}

func TestDiscoverUsesExplicitStateDirAndGatewayPort(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "openclaw.json"), []byte(`{"gateway":{"port":18888,"auth":{"token":"fixture"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCLAW_STATE_DIR", root)
	t.Setenv("OPENCLAW_GATEWAY_PORT", "")
	adapter := New(Config{})
	endpoint, _, err := adapter.discoverEndpoint()
	if err != nil || endpoint != "http://127.0.0.1:18888" {
		t.Fatalf("discoverEndpoint = (%q, %v)", endpoint, err)
	}
	token, _, err := adapter.discoverToken()
	if err != nil || token != "fixture" {
		t.Fatalf("discoverToken = (%q, %v)", token, err)
	}
	t.Setenv("OPENCLAW_GATEWAY_PORT", "19999")
	endpoint, _, err = adapter.discoverEndpoint()
	if err != nil || endpoint != "http://127.0.0.1:19999" {
		t.Fatalf("environment gateway port = (%q, %v)", endpoint, err)
	}
}

func TestProbePreservesUnverifiedDiscoveryReason(t *testing.T) {
	t.Setenv("OPENCLAW_GATEWAY_TOKEN", "test-token")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	capability, err := New(Config{GatewayURL: "http://127.0.0.1:1"}).Probe(ctx)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if capability.Available {
		t.Fatalf("expected unavailable capability, got %+v", capability)
	}
	want := "gateway rpc auth failed"
	if runtime.GOOS == "windows" {
		want = "无法连接 OpenClaw Gateway"
	}
	if !strings.Contains(capability.UnavailableReason, want) {
		t.Fatalf("expected diagnostic reason, got %q", capability.UnavailableReason)
	}
}

func TestDiscoverUsesActiveJSON5ConfigPath(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "custom config.json5")
	if err := os.WriteFile(config, []byte("{ // OpenClaw supports JSON5\n gateway: {port: 19912, auth: {token: 'fixture-token'},},}"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCLAW_CONFIG_PATH", config)
	t.Setenv("OPENCLAW_GATEWAY_PORT", "")
	t.Setenv("OPENCLAW_GATEWAY_TOKEN", "")
	a := New(Config{})
	endpoint, _, err := a.discoverEndpoint()
	if err != nil || endpoint != "http://127.0.0.1:19912" {
		t.Fatalf("endpoint = %q, err = %v", endpoint, err)
	}
	token, _, err := a.discoverToken()
	if err != nil || token != "fixture-token" {
		t.Fatalf("token discovery failed: %v", err)
	}
}

func TestOpenClawHomeAndProfileMatchGatewayDefaults(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OPENCLAW_HOME", root)
	t.Setenv("OPENCLAW_STATE_DIR", "")
	t.Setenv("OPENCLAW_CONFIG_PATH", "")
	t.Setenv("OPENCLAW_PROFILE", "work")
	got, err := openclawConfigPath("")
	want := filepath.Join(root, ".openclaw-work", "openclaw.json")
	if err != nil || got != want {
		t.Fatalf("profile config = %q, want %q, err = %v", got, want, err)
	}
	t.Setenv("OPENCLAW_STATE_DIR", filepath.Join(root, "state"))
	got, err = openclawConfigPath("")
	if err != nil || got != filepath.Join(root, "state", "openclaw.json") {
		t.Fatalf("state override config = %q, err = %v", got, err)
	}
	got, err = openclawConfigPath(filepath.Join(root, "explicit"))
	if err != nil || got != filepath.Join(root, "explicit", "openclaw.json") {
		t.Fatalf("explicit config = %q, err = %v", got, err)
	}
}

func TestGatewayAttachmentsEncodeMaterializedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	attachments, err := gatewayAttachments([]pluginbridge.Attachment{{
		Name: "sample.txt", MIMEType: "text/plain", LocalPath: path,
	}})
	if err != nil {
		t.Fatalf("gatewayAttachments: %v", err)
	}
	item := attachments[0].(map[string]any)
	if item["fileName"] != "sample.txt" || item["content"] != base64.StdEncoding.EncodeToString([]byte("hello")) {
		t.Fatalf("unexpected attachment: %+v", item)
	}
}

func TestGatewayAttachmentsRejectFilesAboveOpenClawDefaultLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.bin")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxGatewayAttachmentBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = gatewayAttachments([]pluginbridge.Attachment{{
		Name: "large.bin", MIMEType: "application/octet-stream", LocalPath: path,
	}})
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected size rejection, got %v", err)
	}
}

func TestFindMarkerEvidenceIncludesAttachmentNameWithoutFabricatingBodyText(t *testing.T) {
	content := []any{map[string]any{
		"role": "user",
		"content": []any{map[string]any{
			"type": "file", "fileName": "native-evidence.txt",
		}},
	}}
	evidence, ok := findMarkerEvidence(content, "native-evidence.txt")
	if !ok || !strings.Contains(evidence, "native-evidence.txt") {
		t.Fatalf("attachment marker not found: evidence=%q ok=%v", evidence, ok)
	}
	if text := flattenText(content); strings.Contains(text, "native-evidence.txt") {
		t.Fatalf("attachment name leaked into body text: %q", text)
	}
}

func TestChatHistoryParamsUseOnlyTheCurrentGatewaySchema(t *testing.T) {
	params := chatHistoryParams(" agent:main:prism:test ", 20)
	if params["sessionKey"] != "agent:main:prism:test" || params["limit"] != 20 || params["maxChars"] != 20000 {
		t.Fatalf("unexpected chat.history params: %+v", params)
	}
	if _, exists := params["sessionId"]; exists {
		t.Fatalf("chat.history must not receive the unsupported sessionId field: %+v", params)
	}
}

func TestGatewayAgentEndPhaseCompletesRun(t *testing.T) {
	event, ok := convertGatewayEvent("agent:main:prism:test", gatewayEventFrame{
		Type:  "event",
		Event: "agent",
		Seq:   7,
		Payload: map[string]any{
			"sessionKey": "agent:main:prism:test",
			"runId":      "run-7",
			"data": map[string]any{
				"phase": "end",
			},
		},
	})
	if !ok {
		t.Fatal("expected agent end event to be converted")
	}
	if event.Type != "run.completed" || event.Status != "completed" {
		t.Fatalf("expected phase=end to complete the run, got type=%q status=%q", event.Type, event.Status)
	}
}

func TestGatewayAgentStartAndEndPhasesProjectTerminalRunState(t *testing.T) {
	started := runFromAgentEvent(map[string]any{"phase": "start"})
	if started["status"] != "running" || started["primary_action"] != "interrupt" {
		t.Fatalf("start phase projection = %+v", started)
	}
	ended := runFromAgentEvent(map[string]any{"phase": "end"})
	if ended["status"] != "completed" || ended["primary_action"] != "send" {
		t.Fatalf("end phase projection = %+v", ended)
	}
}

func TestSubscribePluginConfirmsTerminalRunWhenGatewayOmitsEndEvent(t *testing.T) {
	fake := &fakeRPCClient{
		responses: map[string]map[string]any{
			"agent.wait":   {"status": "completed"},
			"chat.history": {"messages": []any{}},
		},
		events: make(chan gatewayEventFrame, 2),
	}
	adapter := adapterWithFake(fake)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := adapter.SubscribePlugin(ctx)
	if err != nil {
		t.Fatalf("SubscribePlugin: %v", err)
	}
	first := receivePluginEvent(t, events)
	if first.Type != "desktop.session.directory.reconciled" {
		t.Fatalf("initial event = %+v", first)
	}

	fake.events <- gatewayEventFrame{
		Event: "agent",
		Payload: map[string]any{
			"sessionKey": "agent:main:prism:terminal-test",
			"runId":      "run-terminal-test",
			"data":       map[string]any{"phase": "start"},
		},
	}
	started := receivePluginEvent(t, events)
	if started.Type != "run.start" || started.ID != "run-terminal-test" {
		t.Fatalf("started event = %+v", started)
	}
	terminal := receivePluginEvent(t, events)
	if terminal.Type != "run.completed" || terminal.Status != "completed" || terminal.ID != "run-terminal-test:terminal" || nativeSessionID(terminal) != "agent:main:prism:terminal-test" {
		t.Fatalf("terminal event = %+v", terminal)
	}
	if len(fake.calls) == 0 {
		t.Fatal("expected Gateway RPC calls")
	}
	foundWait := false
	for _, call := range fake.calls {
		if call.method == "agent.wait" && call.params["runId"] == "run-terminal-test" {
			foundWait = true
		}
	}
	if !foundWait {
		t.Fatalf("agent.wait was not called for run: %+v", fake.calls)
	}
}

func TestPluginWatcherUsesDirectoryThreadIDWhenEventOmitsIt(t *testing.T) {
	key := "agent:main:dashboard:existing"
	fake := &fakeRPCClient{responses: map[string]map[string]any{
		"sessions.list": {"sessions": []any{map[string]any{"key": key, "sessionId": "native-thread"}}},
	}, events: make(chan gatewayEventFrame, 1)}
	a := adapterWithFake(fake)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := a.SubscribePlugin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = receivePluginEvent(t, events)
	fake.events <- gatewayEventFrame{Event: "agent", Payload: map[string]any{"sessionKey": key, "data": map[string]any{"phase": "running"}}}
	event := receivePluginEvent(t, events)
	native, ok := event.Payload["native_session"].(map[string]any)
	if !ok || native["native_thread_id"] != "native-thread" {
		t.Fatalf("event lost directory identity: %+v", event.Payload)
	}
}

func TestOpenClawSessionHintsReadMillisecondTimestampsAndPreserveGatewayRows(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	hints := openClawSessionHints("ws://127.0.0.1:18789", map[string]any{"sessions": []any{
		map[string]any{"key": "agent:main:prism:one", "label": "One", "updatedAt": now.UnixMilli()},
		map[string]any{"key": "global", "kind": "global", "updatedAt": now.UnixMilli(), "origin": map[string]any{"provider": "webchat"}},
		map[string]any{"key": "agent:main:cron:one", "kind": "cron", "spawnedBy": "cron:daily", "updatedAt": now.UnixMilli()},
	}})
	if len(hints) != 3 || hints[0].NativeSessionID != "agent:main:prism:one" || !hints[0].LastActivityAt.Equal(now) {
		t.Fatalf("unexpected hints: %+v", hints)
	}
	if hints[0].Metadata["agent_id"] != "main" {
		t.Fatalf("agent id was not derived from the session key: %+v", hints[0])
	}
	if hints[1].Metadata["session_kind"] != "global" || hints[1].Metadata["origin_provider"] != "webchat" {
		t.Fatalf("global session metadata was not preserved: %+v", hints[1])
	}
	if hints[2].Metadata["session_kind"] != "cron" || hints[2].Metadata["spawned_by"] != "cron:daily" {
		t.Fatalf("spawned session metadata was not preserved: %+v", hints[2])
	}
}

func TestOpenClawSessionHintNeverUsesGatewayKeyAsPlaceholderTitle(t *testing.T) {
	hint, ok := openClawSessionHint("ws://127.0.0.1:18789", map[string]any{
		"key":          "agent:main:main",
		"derivedTitle": "Untitled",
	})
	if !ok || hint.Title != "" {
		t.Fatalf("expected an empty title for a placeholder title, got %+v", hint)
	}

	renamed, ok := openClawSessionHint("ws://127.0.0.1:18789", map[string]any{
		"key":          "agent:main:main",
		"label":        "New Session",
		"derivedTitle": "Untitled",
	})
	if !ok || renamed.Title != "New Session" {
		t.Fatalf("expected user label to win, got %+v", renamed)
	}
}

func TestListSessionsReadsEveryGatewayPage(t *testing.T) {
	fake := &fakeRPCClient{responses: map[string]map[string]any{
		"sessions.list|offset:0": {
			"sessions":   []any{map[string]any{"key": "agent:main:first", "kind": "direct"}},
			"hasMore":    true,
			"nextOffset": 1,
		},
		"sessions.list|offset:1": {
			"sessions": []any{map[string]any{"key": "agent:main:second", "kind": "direct"}},
			"hasMore":  false,
		},
	}}

	hints, err := listSessionsWithClient(context.Background(), "ws://127.0.0.1:18789", fake)
	if err != nil {
		t.Fatalf("listSessionsWithClient: %v", err)
	}
	if len(hints) != 2 || hints[0].NativeSessionID != "agent:main:first" || hints[1].NativeSessionID != "agent:main:second" {
		t.Fatalf("unexpected paginated hints: %+v", hints)
	}
	if len(fake.calls) != 2 || fake.calls[1].params["offset"] != 1 {
		t.Fatalf("expected two paginated calls, got %+v", fake.calls)
	}
	for _, key := range []string{"includeGlobal", "includeUnknown", "configuredAgentsOnly", "includeDerivedTitles"} {
		if fake.calls[0].params[key] != true {
			t.Fatalf("sessions.list must request %s: %+v", key, fake.calls[0].params)
		}
	}
}

func TestListSessionsRejectsBrokenPagination(t *testing.T) {
	fake := &fakeRPCClient{responses: map[string]map[string]any{
		"sessions.list|offset:0": {
			"sessions":   []any{map[string]any{"key": "agent:main:first"}},
			"hasMore":    true,
			"nextOffset": 0,
		},
	}}

	_, err := listSessionsWithClient(context.Background(), "ws://127.0.0.1:18789", fake)
	if err == nil || !strings.Contains(err.Error(), "invalid nextOffset") {
		t.Fatalf("expected invalid pagination error, got %v", err)
	}
}
