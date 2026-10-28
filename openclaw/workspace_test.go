package openclaw

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	pluginbridge "github.com/Rokid-Prism/prism-plugin-sdk"
)

func TestWorkspaceFallbackUsesOnlyExistingConfiguredAgentDirectory(t *testing.T) {
	root := t.TempDir()
	defaultWorkspace := t.TempDir()
	agentWorkspace := t.TempDir()
	config := map[string]any{"agents": map[string]any{"defaults": map[string]any{"workspace": defaultWorkspace}, "list": []any{map[string]any{"id": "main", "default": true}, map[string]any{"id": "other", "workspace": agentWorkspace}}}}
	encoded, _ := json.Marshal(config)
	if err := os.WriteFile(filepath.Join(root, "openclaw.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	a := New(Config{HomeDir: root})
	if got := a.sessionCwd("agent:main:main", ""); got != defaultWorkspace {
		t.Fatalf("default workspace=%q", got)
	}
	if got := a.sessionCwd("agent:other:chat", ""); got != agentWorkspace {
		t.Fatalf("agent workspace=%q", got)
	}
	if got := a.sessionCwd("agent:main:main", "native-cwd"); got != "native-cwd" {
		t.Fatalf("explicit native cwd overwritten: %q", got)
	}
	event := a.withWorkspaceCwd(pluginbridge.PluginEvent{Payload: map[string]any{"session_hint": map[string]any{"native_session_id": "agent:main:main"}, "native_session": map[string]any{"native_session_id": "agent:main:main"}}})
	for _, key := range []string{"session_hint", "native_session"} {
		if event.Payload[key].(map[string]any)["cwd"] != defaultWorkspace {
			t.Fatalf("event %s lost workspace", key)
		}
	}
	if err := os.RemoveAll(defaultWorkspace); err != nil {
		t.Fatal(err)
	}
	if got := a.sessionCwd("agent:main:main", ""); got != "" {
		t.Fatalf("nonexistent workspace fabricated: %q", got)
	}
}

func TestDraftUsesConfiguredWorkspaceWithoutCreatingSession(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	encoded, _ := json.Marshal(map[string]any{"agents": map[string]any{"defaults": map[string]any{"workspace": workspace}}})
	if err := os.WriteFile(filepath.Join(root, "openclaw.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	a := adapterWithFake(&fakeRPCClient{responses: map[string]map[string]any{}})
	a.Config.HomeDir = root
	opened, err := a.OpenDraft(context.Background(), pluginbridge.DraftOpenRequest{DraftID: "workspace-draft"})
	if err != nil || opened.Cwd != workspace {
		t.Fatalf("draft cwd=%q err=%v", opened.Cwd, err)
	}
}

func TestWorkspaceFallbackUsesExistingDefaultStateWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "openclaw.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a := New(Config{HomeDir: root})
	if got := a.sessionCwd("agent:main:dashboard:example", ""); got != "" {
		t.Fatalf("missing workspace should not be invented: %q", got)
	}
	want := filepath.Join(root, "workspace")
	if err := os.Mkdir(want, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := a.sessionCwd("agent:main:dashboard:example", ""); got != want {
		t.Fatalf("default workspace = %q, want %q", got, want)
	}
	if got := a.sessionCwd("agent:other:chat", ""); got != "" {
		t.Fatalf("other agent must not inherit main workspace: %q", got)
	}
}

func TestWorkspaceRespectsAgentRosterAndProfiles(t *testing.T) {
	root := t.TempDir()
	base := t.TempDir()
	other := filepath.Join(base, "worker")
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	config := map[string]any{"agents": map[string]any{"defaults": map[string]any{"workspace": base}, "entries": map[string]any{"leader": map[string]any{"default": true}, "worker": map[string]any{}}}}
	encoded, _ := json.Marshal(config)
	if err := os.WriteFile(filepath.Join(root, "openclaw.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	a := New(Config{HomeDir: root})
	if got := a.sessionCwd("agent:leader:main", ""); got != base {
		t.Fatalf("default agent = %q", got)
	}
	if got := a.sessionCwd("agent:worker:main", ""); got != other {
		t.Fatalf("other agent = %q", got)
	}
	profile := t.TempDir()
	t.Setenv("OPENCLAW_STATE_DIR", profile)
	t.Setenv("OPENCLAW_CONFIG_PATH", "")
	if err := os.WriteFile(filepath.Join(profile, "openclaw.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(profile, "workspace")
	if err := os.Mkdir(want, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := New(Config{}).sessionCwd("agent:main:dashboard:x", ""); got != want {
		t.Fatalf("profile workspace = %q", got)
	}
}
