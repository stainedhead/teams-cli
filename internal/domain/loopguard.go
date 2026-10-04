package domain

import (
	"fmt"

	"github.com/stainedhead/agent-cli-core/output"
)

// RuleLoopDepth is the rule id of the reply-depth loop guard.
const RuleLoopDepth = "loop.reply_depth"

// CheckLoop denies a send into a thread that already holds depthMax agent
// sends in the reply window (FR-11). depthMax <= 0 disables the guard.
func CheckLoop(depthMax int, sentInThread int) Decision {
	if depthMax > 0 && sentInThread >= depthMax {
		return deny(RuleLoopDepth, fmt.Sprintf("reply depth limit of %d reached in this thread", depthMax))
	}
	return Decision{Allowed: true, Category: output.CategoryOK}
}
