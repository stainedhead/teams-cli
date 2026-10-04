package main

import (
	"context"

	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/usecase"
)

// useCaseDeps is what assemble() hands the use-case layer (architecture s6,
// step 6). Its fields mirror the planned usecase.Deps one to one.
type useCaseDeps struct {
	Policy  usecase.PolicyProvider
	Graph   usecase.Graph
	Ledger  usecase.Ledger
	Cursors usecase.CursorStore
	Audit   usecase.AuditSink
	Clock   usecase.Clock
	Rand    usecase.Rand
	Run     usecase.RunInfo
}

// newCommands is the ONE call site of the use-case constructor.
//
// MERGE NOTE (WS-B): the use-case package does not export New/Deps in this
// worktree yet. After merging WS-B, replace the body with
//
//	return usecase.New(usecase.Deps{Policy: d.Policy, Graph: d.Graph, Ledger: d.Ledger,
//		Cursors: d.Cursors, Audit: d.Audit, Clock: d.Clock, Rand: d.Rand, Run: d.Run})
//
// (adjust field names to WS-B's Deps if they differ) and delete notWired.
func newCommands(useCaseDeps) usecase.Commands { return notWired{} }

// errNotWired is returned by every method of the placeholder.
var errNotWired = domain.NewValidation("use cases are not wired into this build", "merge the use-case workstream and update cmd/teams/usecase_wire.go")

type notWired struct{}

func (notWired) Whoami(context.Context) (usecase.WhoamiResult, error) {
	return usecase.WhoamiResult{}, errNotWired
}

func (notWired) Destinations(context.Context) (usecase.DestinationsResult, error) {
	return usecase.DestinationsResult{}, errNotWired
}

func (notWired) Send(context.Context, usecase.SendRequest) (usecase.SendResult, error) {
	return usecase.SendResult{}, errNotWired
}

func (notWired) Reply(context.Context, usecase.ReplyRequest) (usecase.SendResult, error) {
	return usecase.SendResult{}, errNotWired
}

func (notWired) Inbox(context.Context, usecase.InboxRequest) (usecase.InboxResult, error) {
	return usecase.InboxResult{}, errNotWired
}

func (notWired) Ack(context.Context, usecase.AckRequest) (usecase.AckResult, error) {
	return usecase.AckResult{}, errNotWired
}

func (notWired) ThreadGet(context.Context, usecase.ThreadRequest) (usecase.ThreadResult, error) {
	return usecase.ThreadResult{}, errNotWired
}

func (notWired) Selftest(context.Context, usecase.SelftestRequest) (usecase.SelftestResult, error) {
	return usecase.SelftestResult{}, errNotWired
}
