package openclaw

import (
	"context"
	"fmt"
	"strings"

	pluginbridge "github.com/Rokid-Prism/prism-plugin-sdk"
)

type draftState struct {
	cwd      string
	model    string
	starting bool
}

// Gateway drafts are transient composer state. No native session exists until
// the first message is sent; abandoning a draft therefore leaves no empty chat.
func (a *Adapter) OpenDraft(ctx context.Context, req pluginbridge.DraftOpenRequest) (pluginbridge.DraftOpenResult, error) {
	id := strings.TrimSpace(req.DraftID)
	if id == "" {
		return pluginbridge.DraftOpenResult{}, fmt.Errorf("openclaw: draft id required")
	}
	state := draftState{cwd: a.sessionCwd("agent:main:main", req.Cwd)}
	controls, err := a.draftControls(ctx, state)
	if err != nil {
		return pluginbridge.DraftOpenResult{}, err
	}
	a.draftMu.Lock()
	if a.drafts == nil {
		a.drafts = make(map[string]draftState)
	}
	a.drafts[id] = state
	a.draftMu.Unlock()
	return pluginbridge.DraftOpenResult{DraftID: id, Cwd: state.cwd, Controls: controls, DraftFingerprint: id}, nil
}

func (a *Adapter) draftControls(ctx context.Context, state draftState) (map[string]any, error) {
	client, err := a.dialClient(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	models, err := a.modelsList(ctx, client, "")
	if err != nil {
		return nil, err
	}
	controls := map[string]any{"model_options": modelsToOptions(models), "reasoning_options": []any{}, "actions": []any{map[string]any{"id": "model.switch", "label": "切换模型", "available": len(models) > 0}}}
	if state.model == "" && len(models) == 1 {
		options := modelsToOptions(models)
		controls["current_model"] = options[0]
	}
	if state.model != "" {
		controls["current_model"] = optionObjectWithLabel(state.model, state.model)
	}
	return controls, nil
}

func (a *Adapter) ControlDraft(ctx context.Context, req pluginbridge.DraftControlRequest) (pluginbridge.DraftControlResult, error) {
	id := strings.TrimSpace(req.DraftID)
	a.draftMu.Lock()
	state, ok := a.drafts[id]
	a.draftMu.Unlock()
	if !ok || state.starting {
		return pluginbridge.DraftControlResult{}, fmt.Errorf("draft_stale: OpenClaw draft is no longer available")
	}
	controls, err := a.draftControls(ctx, state)
	if err != nil {
		return pluginbridge.DraftControlResult{}, err
	}
	switch normalizeAction(req.Action) {
	case "controls.describe", "describe.controls":
	case "model.switch":
		target := normalizeOptionTarget(req.Target)
		options, _ := controls["model_options"].([]any)
		if target == "" || !optionKeyExists(options, target) {
			return pluginbridge.DraftControlResult{}, fmt.Errorf("openclaw: model target is not in the current Gateway catalog")
		}
		a.draftMu.Lock()
		current, exists := a.drafts[id]
		if !exists || current.starting {
			a.draftMu.Unlock()
			return pluginbridge.DraftControlResult{}, fmt.Errorf("draft_stale: OpenClaw draft is no longer available")
		}
		current.model = target
		a.drafts[id] = current
		a.draftMu.Unlock()
		controls["current_model"] = optionObjectWithLabel(target, target)
	default:
		return pluginbridge.DraftControlResult{}, fmt.Errorf("openclaw: unsupported draft action %q", req.Action)
	}
	return pluginbridge.DraftControlResult{DraftID: id, Cwd: state.cwd, Controls: controls}, nil
}

func (a *Adapter) StartDraftWithMessage(ctx context.Context, req pluginbridge.StartDraftWithMessageRequest) (pluginbridge.StartSessionWithMessageResult, error) {
	id := strings.TrimSpace(req.DraftID)
	a.draftMu.Lock()
	state, ok := a.drafts[id]
	if !ok || state.starting {
		a.draftMu.Unlock()
		return pluginbridge.StartSessionWithMessageResult{}, fmt.Errorf("draft_stale: OpenClaw draft is no longer available")
	}
	state.starting = true
	a.drafts[id] = state
	a.draftMu.Unlock()
	options := map[string]any{}
	if state.model != "" {
		options["model"] = state.model
	}
	started, err := a.startSessionWithOptions(ctx, pluginbridge.StartSessionWithMessageRequest{PluginID: a.ID(), Cwd: state.cwd, Message: req.Message, SourceDevice: req.SourceDevice, Metadata: req.Metadata}, options)
	a.draftMu.Lock()
	if err == nil {
		delete(a.drafts, id)
	} else {
		state.starting = false
		a.drafts[id] = state
	}
	a.draftMu.Unlock()
	return started, err
}
