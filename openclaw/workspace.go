package openclaw

import (
	"os"
	"path/filepath"
	"strings"

	pluginbridge "github.com/Rokid-Prism/prism-plugin-sdk"
)

// A session directory row need not repeat the agent workspace. Recover it only
// from an explicitly configured, existing directory; never invent a project.
func (a *Adapter) sessionCwd(sessionKey, explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit)
	}
	configPath, err := openclawConfigPath(a.Config.HomeDir)
	if err != nil {
		return ""
	}
	state, err := readOpenClawState(configPath)
	if err != nil {
		return ""
	}
	agentID := openClawAgentIDFromSessionKey(sessionKey)
	if agentID == "" {
		agentID = "main"
	}
	// Honor the roster's explicit/sole default. If a multi-agent roster does
	// not identify an owner, do not guess which agent owns the shared workspace.
	defaultID := ""
	count, marked := 0, 0
	for _, agent := range state.Agents.List {
		if strings.TrimSpace(agent.ID) == "" {
			continue
		}
		count++
		if count == 1 {
			defaultID = agent.ID
		}
		if agent.Default {
			marked++
			defaultID = agent.ID
		}
	}
	if state.Agents.Entries != nil {
		count, marked, defaultID = 0, 0, ""
		for id, agent := range state.Agents.Entries {
			count++
			if count == 1 {
				defaultID = id
			}
			if agent.Default {
				marked++
				defaultID = id
			}
		}
	}
	if count == 0 {
		defaultID = "main"
	} else if count > 1 && marked != 1 {
		defaultID = ""
	}
	workspace := ""
	if agent, ok := state.Agents.Entries[agentID]; ok {
		workspace = agent.Workspace
	}
	if state.Agents.Entries == nil {
		for _, agent := range state.Agents.List {
			if agent.ID == agentID {
				workspace = agent.Workspace
				break
			}
		}
	}
	workspace = strings.TrimSpace(workspace)
	if workspace == "" && defaultID != "" {
		workspace = strings.TrimSpace(state.Agents.Defaults.Workspace)
		if workspace != "" && agentID != defaultID && filepath.Base(agentID) == agentID {
			workspace = filepath.Join(workspace, agentID)
		}
	}
	// OpenClaw's default agent uses the workspace beneath its state directory
	// when no workspace is configured. Only project it when it exists; other
	// agents can have different default workspace rules.
	if workspace == "" && agentID == defaultID {
		workspace = strings.TrimSpace(os.Getenv("OPENCLAW_WORKSPACE_DIR"))
	}
	if workspace == "" && agentID == defaultID {
		if stateDir, err := openclawHome(a.Config.HomeDir); err == nil {
			workspace = filepath.Join(stateDir, "workspace")
		}
	}
	if workspace == "~" || strings.HasPrefix(workspace, "~/") || strings.HasPrefix(workspace, "~\\") {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		workspace = filepath.Join(home, strings.TrimLeft(workspace[1:], "/\\"))
	}
	if !filepath.IsAbs(workspace) {
		return ""
	}
	info, err := os.Stat(workspace)
	if err != nil || !info.IsDir() {
		return ""
	}
	return filepath.Clean(workspace)
}

func (a *Adapter) withWorkspaceCwd(event pluginbridge.PluginEvent) pluginbridge.PluginEvent {
	for _, field := range []string{"session_hint", "native_session"} {
		if row, ok := event.Payload[field].(map[string]any); ok {
			row["cwd"] = a.sessionCwd(stringFromAny(row["native_session_id"]), stringFromAny(row["cwd"]))
		}
	}
	return event
}
