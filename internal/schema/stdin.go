// Package schema mirror JSON document Claude Code write to status line
// command's stdin.
//
// Every documented-optional field is a pointer here. Absent or null in normal
// operation: rate_limits for non-subscribers, current_usage before first API
// call and again after /compact, used_percentage early in a session, effort on
// models carrying no such parameter. Segments must tell absent from zero, so
// that distinction live in type, not at each call site.
package schema

import (
	"encoding/json"
	"math"
)

type Input struct {
	CWD            string  `json:"cwd"`
	SessionID      string  `json:"session_id"`
	SessionName    *string `json:"session_name"`
	PromptID       *string `json:"prompt_id"`
	TranscriptPath string  `json:"transcript_path"`
	Version        string  `json:"version"`

	Model       Model        `json:"model"`
	Workspace   Workspace    `json:"workspace"`
	OutputStyle *OutputStyle `json:"output_style"`
	Cost        *Cost        `json:"cost"`
	Context     *ContextWin  `json:"context_window"`
	Effort      *Effort      `json:"effort"`
	Thinking    *Thinking    `json:"thinking"`
	RateLimits  *RateLimits  `json:"rate_limits"`
	Vim         *Vim         `json:"vim"`
	Agent       *Agent       `json:"agent"`
	PR          *PullRequest `json:"pr"`
	Worktree    *Worktree    `json:"worktree"`

	Exceeds200k *bool `json:"exceeds_200k_tokens"`
	FastMode    *bool `json:"fast_mode"`
}

type Model struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type Workspace struct {
	CurrentDir  string   `json:"current_dir"`
	ProjectDir  string   `json:"project_dir"`
	AddedDirs   []string `json:"added_dirs"`
	GitWorktree *string  `json:"git_worktree"`
	Repo        *Repo    `json:"repo"`
}

type Repo struct {
	Host  string `json:"host"`
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

type OutputStyle struct {
	Name string `json:"name"`
}

type Cost struct {
	TotalCostUSD       *float64 `json:"total_cost_usd"`
	TotalDurationMS    *int64   `json:"total_duration_ms"`
	TotalAPIDurationMS *int64   `json:"total_api_duration_ms"`
	TotalLinesAdded    *int64   `json:"total_lines_added"`
	TotalLinesRemoved  *int64   `json:"total_lines_removed"`
}

// ContextWin is live context window from most recent API response. As of Claude
// Code v2.1.132 TotalInputTokens and TotalOutputTokens report current occupancy,
// not cumulative session totals -- those come from transcript.
type ContextWin struct {
	TotalInputTokens  *int64   `json:"total_input_tokens"`
	TotalOutputTokens *int64   `json:"total_output_tokens"`
	ContextWindowSize *int64   `json:"context_window_size"`
	UsedPercentage    *float64 `json:"used_percentage"`
	RemainingPct      *float64 `json:"remaining_percentage"`
	CurrentUsage      *Usage   `json:"current_usage"`
}

type Usage struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadTokens     int64 `json:"cache_read_input_tokens"`
}

type Effort struct {
	Level string `json:"level"`
}

type Thinking struct {
	Enabled bool `json:"enabled"`
}

// RateLimits reach Claude.ai subscribers only, and only after first API
// response of a session. Each window go absent independently.
type RateLimits struct {
	FiveHour *Window `json:"five_hour"`
	SevenDay *Window `json:"seven_day"`
}

type Window struct {
	UsedPercentage float64 `json:"used_percentage"`
	ResetsAt       *int64  `json:"resets_at"`
}

type Vim struct {
	Mode string `json:"mode"`
}

type Agent struct {
	Name string `json:"name"`
}

type PullRequest struct {
	Number      *int64  `json:"number"`
	URL         *string `json:"url"`
	ReviewState *string `json:"review_state"`
}

type Worktree struct {
	Name           *string `json:"name"`
	Path           *string `json:"path"`
	Branch         *string `json:"branch"`
	OriginalCWD    *string `json:"original_cwd"`
	OriginalBranch *string `json:"original_branch"`
}

// Parse decode stdin document. Unknown fields ignored, so a newer Claude Code
// release break no rendering.
func Parse(b []byte) (*Input, error) {
	var in Input
	if err := json.Unmarshal(b, &in); err != nil {
		return nil, err
	}
	return &in, nil
}

// Dir to display. Docs prefer workspace.current_dir, matching
// workspace.project_dir. Top-level cwd hold same value, so it fall back.
func (in *Input) Dir() string {
	if in.Workspace.CurrentDir != "" {
		return in.Workspace.CurrentDir
	}
	return in.CWD
}

// ContextPercent report window utilisation, plus whether it is known at all.
//
// used_percentage go null early in a session, so derive from current_usage,
// itself null before first API call and after /compact. Neither available =
// caller omit segment, never print a misleading zero.
func (in *Input) ContextPercent() (float64, bool) {
	if in.Context == nil {
		return 0, false
	}
	if p := in.Context.UsedPercentage; p != nil {
		// NaN clear p<0 and p>100 alike, so clampPercent hand it straight on,
		// and int(NaN) is minInt64 on amd64. Unknown, not zero.
		if math.IsNaN(*p) {
			return 0, false
		}
		return clampPercent(*p), true
	}
	u := in.Context.CurrentUsage
	if u == nil || in.Context.ContextWindowSize == nil || *in.Context.ContextWindowSize <= 0 {
		return 0, false
	}
	// OutputTokens left out on purpose: absent from total_input_tokens too, and
	// Claude Code's own used_percentage match this input-only sum.
	used := u.InputTokens + u.CacheCreationTokens + u.CacheReadTokens
	pct := float64(used) * 100 / float64(*in.Context.ContextWindowSize)
	return clampPercent(pct), true
}

// clampPercent hold value in 0..100. context_window_size stale against usage
// counts, or used_percentage over 100, else render past full bar.
func clampPercent(p float64) float64 {
	switch {
	case p < 0:
		return 0
	case p > 100:
		return 100
	default:
		return p
	}
}
