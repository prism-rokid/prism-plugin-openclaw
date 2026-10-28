package openclaw

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	pluginbridge "github.com/Rokid-Prism/prism-plugin-sdk"
)

// A single model request for validating a freshly installed, isolated Gateway.
func TestLocalGatewayMinimalMessageE2E(t *testing.T) {
	if os.Getenv("PRISM_OPENCLAW_MESSAGE_E2E") != "1" {
		t.Skip("set PRISM_OPENCLAW_MESSAGE_E2E=1 with an authenticated Gateway and model configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	a := New(Config{GatewayURL: os.Getenv("OPENCLAW_GATEWAY_URL")})
	capability, err := a.Probe(ctx)
	if err != nil || !capability.Available {
		t.Fatalf("gateway unavailable: %s (%v)", capability.UnavailableReason, err)
	}
	marker := "PRISM_GATEWAY_OK"
	firstMessage := pluginbridge.InboundMessage{
		PrismMessageID: fmt.Sprintf("minimal-e2e-%d", time.Now().UnixNano()),
		Text:           "Reply with exactly " + marker + ". Do not use tools.",
		Timestamp:      time.Now().UTC(),
	}
	if os.Getenv("PRISM_OPENCLAW_E2E_ZH") == "1" {
		firstMessage.Text = "只回复 " + marker + "，不要使用工具。"
	}
	var started pluginbridge.StartSessionWithMessageResult
	if os.Getenv("PRISM_OPENCLAW_DRAFT_E2E") == "1" {
		draftID := "draft-" + firstMessage.PrismMessageID
		opened, openErr := a.OpenDraft(ctx, pluginbridge.DraftOpenRequest{DraftID: draftID, PluginID: "openclaw"})
		if openErr != nil || opened.DraftID != draftID {
			t.Fatalf("open draft: %v", openErr)
		}
		if _, controlErr := a.ControlDraft(ctx, pluginbridge.DraftControlRequest{DraftID: draftID, Action: "controls.describe"}); controlErr != nil {
			t.Fatal(controlErr)
		}
		started, err = a.StartDraftWithMessage(ctx, pluginbridge.StartDraftWithMessageRequest{DraftID: draftID, PluginID: "openclaw", Message: firstMessage})
		if err == nil {
			if _, retryErr := a.StartDraftWithMessage(ctx, pluginbridge.StartDraftWithMessageRequest{DraftID: draftID, Message: firstMessage}); retryErr == nil || !strings.Contains(retryErr.Error(), "draft_stale") {
				t.Fatalf("consumed draft retry: %v", retryErr)
			}
		}
	} else {
		started, err = a.StartSessionWithMessage(ctx, pluginbridge.StartSessionWithMessageRequest{
			PluginID: "openclaw",
			Message:  firstMessage,
		})
	}
	if err != nil {
		t.Fatalf("start message: %v", err)
	}
	defer deleteE2ESession(t, a, started.Session.NativeSessionID)
	terminal, err := a.WaitForRun(ctx, started.Session, started.Receipt.NativeMessageID)
	if err != nil || terminal.Type != "run.completed" {
		t.Fatalf("model did not complete: type=%s summary=%s err=%v", terminal.Type, terminal.Summary, err)
	}
	client, err := a.dialClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	history, err := client.Request(ctx, "chat.history", chatHistoryParams(started.Session.NativeSessionID, 20))
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range firstArray(history, "messages") {
		message, _ := raw.(map[string]any)
		if stringFromAny(message["role"]) == "assistant" && strings.Contains(flattenText(message["content"]), marker) {
			return
		}
	}
	t.Fatal("completed model run has no expected assistant reply in native history")
}

func TestLocalGatewayE2E(t *testing.T) {
	if os.Getenv("PRISM_OPENCLAW_E2E") != "1" {
		t.Skip("set PRISM_OPENCLAW_E2E=1 with a local OpenClaw Gateway running")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	a := New(Config{GatewayURL: "http://127.0.0.1:18789"})
	capability, err := a.Probe(ctx)
	if err != nil || !capability.Available {
		t.Fatalf("Probe capability=%+v err=%v", capability, err)
	}
	if sessions, err := a.ListSessions(ctx); err != nil || len(sessions) == 0 {
		t.Fatalf("ListSessions count=%d err=%v", len(sessions), err)
	}

	watchCtx, stopWatch := context.WithCancel(ctx)
	defer stopWatch()
	events, err := a.SubscribePlugin(watchCtx)
	if err != nil {
		t.Fatalf("SubscribePlugin: %v", err)
	}
	if event := receivePluginEvent(t, events); event.Type != "desktop.session.directory.reconciled" {
		t.Fatalf("initial watcher event: %+v", event)
	}

	key := fmt.Sprintf("agent:main:prism:e2e-%d", time.Now().UnixNano())
	client, err := a.dialClient(ctx)
	if err != nil {
		t.Fatalf("dialClient: %v", err)
	}
	if _, err := client.Request(ctx, "sessions.create", map[string]any{
		"key": key, "agentId": "main", "label": "Prism OpenClaw E2E",
	}); err != nil {
		client.Close()
		t.Fatalf("sessions.create: %v", err)
	}
	client.Close()
	defer func() {
		cleanup, cleanupErr := a.dialClient(context.Background())
		if cleanupErr == nil {
			_, _ = cleanup.Request(context.Background(), "sessions.delete", map[string]any{"key": key, "deleteTranscript": true})
			_ = cleanup.Close()
		}
	}()

	session := pluginbridge.NativeSession{
		PluginID: "openclaw", NativeSessionID: key, Surface: "gateway",
		Endpoint: "http://127.0.0.1:18789", Visible: true,
	}
	described, err := a.ControlSession(ctx, pluginbridge.ControlSessionRequest{Session: session, Action: "controls.describe"})
	if err != nil || len(described.Details) == 0 {
		t.Fatalf("controls.describe details=%+v err=%v", described.Details, err)
	}
	if _, exists := described.Details["permission_options"]; exists {
		t.Fatalf("OpenClaw must not publish unverifiable permission options: %+v", described.Details)
	}

	modelOptions, _ := described.Details["model_options"].([]any)
	if len(modelOptions) == 0 {
		t.Fatal("expected at least one available model")
	}
	modelKey := stringFromAny(modelOptions[0].(map[string]any)["key"])
	if _, err := a.ControlSession(ctx, pluginbridge.ControlSessionRequest{Session: session, Action: "model.switch", Target: modelKey}); err != nil {
		t.Fatalf("model.switch %s: %v", modelKey, err)
	}

	reasoningOptions, _ := described.Details["reasoning_options"].([]any)
	if len(reasoningOptions) > 0 {
		reasoningKey := stringFromAny(reasoningOptions[0].(map[string]any)["key"])
		if _, err := a.ControlSession(ctx, pluginbridge.ControlSessionRequest{Session: session, Action: "reasoning.switch", Target: reasoningKey}); err != nil {
			t.Fatalf("reasoning.switch %s: %v", reasoningKey, err)
		}
	}

	for _, control := range []pluginbridge.ControlSessionRequest{
		{Session: session, Action: "rename", Name: "Prism OpenClaw E2E Renamed"},
		{Session: session, Action: "pin", Target: map[string]any{"enabled": true}},
		{Session: session, Action: "context.compact"},
	} {
		result, err := a.ControlSession(ctx, control)
		if err != nil || !result.OK || !result.DetailsConfirmed {
			t.Fatalf("%s result=%+v err=%v", control.Action, result, err)
		}
	}

	foundIndex := false
	deadline := time.After(8 * time.Second)
	for !foundIndex {
		select {
		case event := <-events:
			if event.Type == "desktop.session.index.changed" && nativeSessionID(event) == key {
				foundIndex = true
			}
		case <-deadline:
			t.Fatal("wide watcher did not emit the E2E session index update")
		}
	}

	if result, err := a.ControlSession(ctx, pluginbridge.ControlSessionRequest{Session: session, Action: "archive"}); err != nil || !result.DetailsConfirmed {
		t.Fatalf("archive result=%+v err=%v", result, err)
	}
	if result, err := a.ControlSession(ctx, pluginbridge.ControlSessionRequest{Session: session, Action: "delete"}); err != nil || !result.DetailsConfirmed {
		t.Fatalf("delete result=%+v err=%v", result, err)
	}

	client, err = a.dialClient(ctx)
	if err != nil {
		t.Fatalf("dial after delete: %v", err)
	}
	defer client.Close()
	list, err := client.Request(ctx, "sessions.list", map[string]any{"limit": 200})
	if err != nil {
		t.Fatalf("sessions.list after delete: %v", err)
	}
	rows, _ := list["sessions"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if stringFromAny(row["key"]) == key {
			t.Fatalf("deleted E2E session still present: %s", key)
		}
	}
}

func TestLocalGatewayPeerSyncE2E(t *testing.T) {
	if os.Getenv("PRISM_OPENCLAW_E2E") != "1" {
		t.Skip("set PRISM_OPENCLAW_E2E=1 with a local OpenClaw Gateway running")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	a := New(Config{GatewayURL: "http://127.0.0.1:18789"})

	watchCtx, stopWatch := context.WithCancel(ctx)
	defer stopWatch()
	events, err := a.SubscribePlugin(watchCtx)
	if err != nil {
		t.Fatalf("SubscribePlugin: %v", err)
	}
	if event := receivePluginEvent(t, events); event.Type != "desktop.session.directory.reconciled" {
		t.Fatalf("initial watcher event: %+v", event)
	}

	peer, err := a.dialClient(ctx)
	if err != nil {
		t.Fatalf("dial peer client: %v", err)
	}
	defer peer.Close()
	stamp := time.Now().UnixNano()
	key := fmt.Sprintf("agent:main:prism:peer-e2e-%d", stamp)
	if _, err := peer.Request(ctx, "sessions.create", map[string]any{
		"key": key, "agentId": "main", "label": "Prism OpenClaw Peer E2E",
	}); err != nil {
		t.Fatalf("peer sessions.create: %v", err)
	}
	defer deleteE2ESession(t, a, key)

	index := receivePluginEventMatching(t, events, 8*time.Second, func(event pluginbridge.PluginEvent) bool {
		return event.Type == "desktop.session.index.changed" && nativeSessionID(event) == key
	})
	if index.Type == "" {
		t.Fatal("plugin watcher did not observe peer session creation")
	}

	peerMarker := fmt.Sprintf("PRISM_OPENCLAW_PEER_%d", stamp)
	peerRun, err := peer.Request(ctx, "chat.send", map[string]any{
		"sessionKey": key, "message": peerMarker, "deliver": false,
		"idempotencyKey": "peer-" + strconv.FormatInt(stamp, 10),
	})
	if err != nil {
		t.Fatalf("peer chat.send: %v", err)
	}
	peerEvent := receivePluginEventMatching(t, events, 10*time.Second, func(event pluginbridge.PluginEvent) bool {
		return event.Type == "message.user" && nativeSessionID(event) == key && strings.Contains(event.Summary, peerMarker)
	})
	if peerEvent.Type == "" {
		t.Fatal("plugin watcher did not observe the peer user message")
	}
	if runID := stringFromAny(peerRun["runId"]); runID != "" {
		waitTimeout := 90 * time.Second
		waitCtx, cancelWait := context.WithTimeout(ctx, waitTimeout)
		waitResult, waitErr := peer.RequestWithTimeout(waitCtx, "agent.wait", map[string]any{
			"runId": runID, "timeoutMs": int(waitTimeout / time.Millisecond),
		}, waitTimeout+2*time.Second)
		cancelWait()
		if waitErr != nil {
			t.Fatalf("peer agent.wait: %v", waitErr)
		}
		status := normalizeWaitStatus(stringFromMap(waitResult, "status"))
		if status != "completed" && status != "failed" {
			t.Fatalf("peer run did not reach terminal state: %+v", waitResult)
		}
	}

	prismMarker := fmt.Sprintf("PRISM_OPENCLAW_PLUGIN_%d", stamp)
	receipt, err := a.Send(ctx, pluginbridge.NativeSession{
		PluginID: "openclaw", NativeSessionID: key, Surface: "gateway",
	}, pluginbridge.InboundMessage{
		PrismMessageID: "plugin-" + strconv.FormatInt(stamp, 10),
		Text:           prismMarker,
		SourceDevice:   "prism-openclaw-peer-e2e",
		Timestamp:      time.Now().UTC(),
	})
	if err != nil || !receipt.Accepted {
		t.Fatalf("plugin Send receipt=%+v err=%v", receipt, err)
	}
	defer func() {
		if receipt.NativeMessageID != "" {
			_, _ = peer.Request(context.Background(), "chat.abort", map[string]any{
				"sessionKey": key, "runId": receipt.NativeMessageID,
			})
		}
	}()

	deadline := time.Now().Add(8 * time.Second)
	for {
		history, historyErr := peer.Request(ctx, "chat.history", map[string]any{"sessionKey": key, "limit": 20})
		if historyErr == nil && strings.Contains(flattenVisibilityText(history["messages"]), prismMarker) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("peer history did not observe plugin message: history=%+v err=%v", history, historyErr)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestLocalGatewayConversationE2E(t *testing.T) {
	if os.Getenv("PRISM_OPENCLAW_E2E") != "1" {
		t.Skip("set PRISM_OPENCLAW_E2E=1 with a local OpenClaw Gateway running")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	a := New(Config{GatewayURL: "http://127.0.0.1:18789"})
	pluginEvents, err := a.SubscribePlugin(ctx)
	if err != nil {
		t.Fatalf("SubscribePlugin: %v", err)
	}
	if event := receivePluginEvent(t, pluginEvents); event.Type != "desktop.session.directory.reconciled" {
		t.Fatalf("initial plugin event = %+v", event)
	}

	stamp := time.Now().UnixNano()
	messageID := fmt.Sprintf("openclaw-conversation-e2e-%d", stamp)
	firstMarker := fmt.Sprintf("PRISM_OPENCLAW_FIRST_%d", stamp)
	attachmentPath := filepath.Join(t.TempDir(), "prism-openclaw-e2e.txt")
	if err := os.WriteFile(attachmentPath, []byte("PRISM_OPENCLAW_ATTACHMENT_OK\n"), 0o600); err != nil {
		t.Fatalf("write attachment: %v", err)
	}

	started, err := a.StartSessionWithMessage(ctx, pluginbridge.StartSessionWithMessageRequest{
		PluginID: "openclaw",
		Message: pluginbridge.InboundMessage{
			PrismMessageID: messageID,
			Text:           "Reply only " + firstMarker,
			Attachments: []pluginbridge.Attachment{{
				Name: "prism-openclaw-e2e.txt", MIMEType: "text/plain", LocalPath: attachmentPath,
			}},
			SourceDevice: "prism-openclaw-e2e",
			Timestamp:    time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("StartSessionWithMessage: %v", err)
	}
	session := started.Session
	if !started.Receipt.Accepted || !started.Receipt.Visible || !started.Visibility.Visible {
		t.Fatalf("start receipt=%+v visibility=%+v", started.Receipt, started.Visibility)
	}
	defer deleteE2ESession(t, a, session.NativeSessionID)

	terminal, err := a.WaitForRun(ctx, session, started.Receipt.NativeMessageID)
	if err != nil {
		t.Fatalf("WaitForRun: %v", err)
	}
	if terminal.Type != "run.completed" && terminal.Type != "run.failed" {
		t.Fatalf("unexpected terminal event: %+v", terminal)
	}
	assertPluginRunTerminal(t, pluginEvents, session.NativeSessionID)

	historyCtx, stopHistory := context.WithCancel(ctx)
	historyEvents, err := a.ReadHistoryStream(historyCtx, session, pluginbridge.HistoryStreamRequest{
		StreamID: "openclaw-conversation-e2e-history", Limit: 5, Live: true,
	})
	if err != nil {
		t.Fatalf("ReadHistoryStream: %v", err)
	}
	defer stopHistory()
	initialRendered := ""
	for {
		event := receiveHistoryEvent(t, historyEvents)
		if event.Turn != nil {
			initialRendered += fmt.Sprint(event.Turn)
		}
		if event.Type == "page_end" {
			break
		}
	}
	if !strings.Contains(initialRendered, firstMarker) {
		t.Fatalf("initial history stream does not contain first marker: %s", initialRendered)
	}

	secondMarker := fmt.Sprintf("PRISM_OPENCLAW_SECOND_%d", stamp)
	second, err := a.Send(ctx, session, pluginbridge.InboundMessage{
		PrismMessageID: messageID + "-second",
		Text:           "Reply only " + secondMarker,
		SourceDevice:   "prism-openclaw-e2e",
		Timestamp:      time.Now().UTC(),
	})
	if err != nil || !second.Accepted || !second.Visible {
		t.Fatalf("second Send receipt=%+v err=%v", second, err)
	}
	liveDeadline := time.After(15 * time.Second)
	for {
		select {
		case event, ok := <-historyEvents:
			if !ok {
				t.Fatal("live history stream closed before second marker")
			}
			if event.Type == "error" {
				t.Fatalf("live history stream failed: %+v", event)
			}
			if event.Source == "live" && event.Turn != nil && strings.Contains(fmt.Sprint(event.Turn), secondMarker) {
				stopHistory()
				goto liveHistoryObserved
			}
		case <-liveDeadline:
			t.Fatal("live history stream did not publish the second marker")
		}
	}

liveHistoryObserved:
	waitForHistoryStreamClose(t, historyEvents)
	visible, err := a.VerifyVisibility(ctx, session, secondMarker)
	if err != nil || !visible.Visible {
		t.Fatalf("second visibility=%+v err=%v", visible, err)
	}
	terminal, err = a.WaitForRun(ctx, session, second.NativeMessageID)
	if err != nil {
		t.Fatalf("WaitForRun after second Send: %v", err)
	}
	if terminal.Type != "run.completed" && terminal.Type != "run.failed" {
		t.Fatalf("unexpected terminal event after second Send: %+v", terminal)
	}
	assertPluginRunTerminal(t, pluginEvents, session.NativeSessionID)

	history, err := a.ReadHistory(ctx, session, 20)
	if err != nil {
		t.Fatalf("ReadHistory: %v", err)
	}
	rendered := fmt.Sprint(history)
	if !strings.Contains(rendered, firstMarker) || !strings.Contains(rendered, secondMarker) {
		t.Fatalf("history does not contain both user markers: %s", rendered)
	}
	if strings.Contains(rendered, attachmentPath) {
		t.Fatalf("history leaked the materialized attachment path: %s", attachmentPath)
	}
}

func assertPluginRunTerminal(t *testing.T, events <-chan pluginbridge.PluginEvent, sessionKey string) {
	t.Helper()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("plugin-wide subscription closed before run terminal")
			}
			if nativeSessionID(event) == sessionKey && (event.Type == "run.completed" || event.Type == "run.failed") {
				return
			}
		case <-deadline.C:
			t.Fatalf("plugin-wide subscription did not publish a terminal event for %s", sessionKey)
		}
	}
}

func waitForHistoryStreamClose(t *testing.T, events <-chan pluginbridge.HistoryStreamEvent) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return
			}
		case <-deadline.C:
			t.Fatal("history stream did not close after cancellation")
		}
	}
}

func receivePluginEventMatching(t *testing.T, events <-chan pluginbridge.PluginEvent, timeout time.Duration, match func(pluginbridge.PluginEvent) bool) pluginbridge.PluginEvent {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("plugin event stream closed before the expected event")
			}
			if match(event) {
				return event
			}
		case <-deadline.C:
			t.Fatal("timed out waiting for matching plugin event")
			return pluginbridge.PluginEvent{}
		}
	}
}

func deleteE2ESession(t *testing.T, a *Adapter, sessionKey string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := a.dialClient(ctx)
	if err != nil {
		t.Errorf("cleanup dial: %v", err)
		return
	}
	defer client.Close()
	if _, err := client.Request(ctx, "sessions.delete", map[string]any{
		"key": sessionKey, "deleteTranscript": true,
	}); err != nil {
		t.Errorf("cleanup session %s: %v", sessionKey, err)
	}
}
