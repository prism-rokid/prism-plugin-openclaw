package openclaw

import (
	"context"
	"strings"
	"testing"

	pluginbridge "github.com/Rokid-Prism/prism-plugin-sdk"
)

func TestDraftOpenAndControlDoNotCreateNativeSessions(t *testing.T) {
	fake := &fakeRPCClient{responses: map[string]map[string]any{"models.list": {"models": []any{map[string]any{"id": "MiniMax-M3", "provider": "minimax", "name": "MiniMax-M3"}}}}}
	a := adapterWithFake(fake)
	opened, err := a.OpenDraft(context.Background(), pluginbridge.DraftOpenRequest{DraftID: "draft-one", Cwd: ""})
	if err != nil || opened.DraftID != "draft-one" || opened.DraftFingerprint == "" {
		t.Fatalf("open draft=%+v err=%v", opened, err)
	}
	if opened.Controls["current_model"] == nil {
		t.Fatal("single catalog model was not reflected in controls")
	}
	controlled, err := a.ControlDraft(context.Background(), pluginbridge.DraftControlRequest{DraftID: "draft-one", Action: "model.switch", Target: "minimax/MiniMax-M3"})
	if err != nil || controlled.Controls["current_model"] == nil {
		t.Fatalf("model control=%+v err=%v", controlled, err)
	}
	if _, err = a.ControlDraft(context.Background(), pluginbridge.DraftControlRequest{DraftID: "draft-one", Action: "model.switch", Target: "invalid"}); err == nil {
		t.Fatal("invalid model accepted")
	}
	for _, call := range fake.calls {
		if call.method != "models.list" {
			t.Fatalf("draft mutated native state via %s", call.method)
		}
	}
}

func TestUnknownAndStartingDraftsAreStale(t *testing.T) {
	a := New(Config{})
	if _, err := a.ControlDraft(context.Background(), pluginbridge.DraftControlRequest{DraftID: "missing", Action: "controls.describe"}); err == nil || !strings.Contains(err.Error(), "draft_stale") {
		t.Fatalf("missing draft err=%v", err)
	}
	a.drafts = map[string]draftState{"starting": {starting: true}}
	if _, err := a.StartDraftWithMessage(context.Background(), pluginbridge.StartDraftWithMessageRequest{DraftID: "starting"}); err == nil || !strings.Contains(err.Error(), "draft_stale") {
		t.Fatalf("starting draft err=%v", err)
	}
}

func TestDraftEmptyFirstMessagePreservesDraftWithoutCreatingSession(t *testing.T) {
	a := New(Config{})
	a.drafts = map[string]draftState{"draft-empty": {cwd: "fixture"}}
	if _, err := a.StartDraftWithMessage(context.Background(), pluginbridge.StartDraftWithMessageRequest{DraftID: "draft-empty"}); err == nil {
		t.Fatal("empty message was accepted")
	}
	state, ok := a.drafts["draft-empty"]
	if !ok || state.starting || state.cwd != "fixture" {
		t.Fatalf("draft lost after validation failure: %+v", state)
	}
}
