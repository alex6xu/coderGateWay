package agentlink

import (
	"github.com/alex/codegateway/internal/agent/promptctx"
	"github.com/alex/codegateway/internal/agentcore"
	"github.com/alex/codegateway/internal/compaction"
)

// CompactToolResults applies CodeGateway-style tool-result clipping to an
// agentcore message list: keep the newest keepRecent tool results at maxChars,
// and aggressively shorten older ones. Mutates msgs in place.
func CompactToolResults(msgs agentcore.MessageList, keepRecent, maxChars int) {
	if keepRecent <= 0 {
		keepRecent = 2
	}
	if maxChars <= 0 {
		maxChars = 4000
	}

	toolIdx := make([]int, 0)
	for i, m := range msgs {
		if _, ok := m.(agentcore.ToolResultMessage); ok {
			toolIdx = append(toolIdx, i)
		}
	}
	if len(toolIdx) == 0 {
		return
	}

	clip := func(i, limit int, older bool) {
		tr := msgs[i].(agentcore.ToolResultMessage)
		text := agentcore.ContentToText(tr.Content)
		var next string
		if older {
			if len(text) <= 240 {
				return
			}
			next = text[:240] + "\n…[older tool result compacted]"
		} else {
			next = promptctx.TruncateToolResult(text, limit)
			if next == text {
				return
			}
		}
		tr.Content = agentcore.ContentList{agentcore.NewTextContent(next)}
		msgs[i] = tr
	}

	if len(toolIdx) <= keepRecent {
		for _, i := range toolIdx {
			text := agentcore.ContentToText(msgs[i].(agentcore.ToolResultMessage).Content)
			if len(text) > maxChars*2 {
				clip(i, maxChars, false)
			}
		}
		return
	}

	cutoff := len(toolIdx) - keepRecent
	for n, i := range toolIdx {
		if n < cutoff {
			clip(i, maxChars, true)
			continue
		}
		text := agentcore.ContentToText(msgs[i].(agentcore.ToolResultMessage).Content)
		if len(text) > maxChars {
			clip(i, maxChars, false)
		}
	}
}

// CompactionSettingsFromBudget maps CodeGateway context knobs onto pigo
// CompactionSettings. compactRatio (e.g. 0.75) decides when to fire:
// tokens > window*(ratio) ≈ window - reserve. targetRatio (e.g. 0.55) sizes the
// retained recent window after a compaction.
func CompactionSettingsFromBudget(window int, compactRatio, targetRatio float64) (contextWindow int, settings compaction.CompactionSettings) {
	if window <= 0 {
		window = 8000
	}
	if compactRatio <= 0 || compactRatio >= 1 {
		compactRatio = 0.75
	}
	if targetRatio <= 0 || targetRatio >= compactRatio {
		targetRatio = 0.55
		if targetRatio >= compactRatio {
			targetRatio = compactRatio * 0.7
		}
	}

	reserve := int(float64(window) * (1 - compactRatio))
	if reserve < 256 {
		reserve = 256
	}
	keep := int(float64(window) * targetRatio)
	usable := window - reserve
	if keep <= 0 {
		keep = usable
	}
	if keep > usable {
		keep = usable
	}
	if keep < 256 {
		keep = 256
	}

	return window, compaction.CompactionSettings{
		Enabled:          true,
		ReserveTokens:    reserve,
		KeepRecentTokens: keep,
	}
}
