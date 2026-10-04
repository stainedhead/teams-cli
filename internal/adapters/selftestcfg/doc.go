// Package selftestcfg builds the allow/deny matrix for `teams selftest` (spec
// s8) from the loaded policy and the probe that drives it through
// usecase.Commands and, for the two rows that have no command, the Graph port.
//
// Rows are derived from the policy so the matrix describes the policy actually
// in force: a row is present only when its feature is configured (a
// classification row needs markers, a negative-membership row needs
// selftest.non_member_chat_id, ...). Only send-allowed writes; every other row
// is read-only or a dry run, and `--read-only` skips send-allowed.
//
// The probe maps a nil error to Allow and a refusal category to Deny. Any other
// error fails the row with the error text, so a broken backend is never
// mistaken for a policy decision.
package selftestcfg
