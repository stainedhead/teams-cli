// Package state persists the idempotency ledger and the per-destination
// cursors in the state directory (D7, NFR-7). Both files are versioned JSON,
// written atomically (temp file, fsync, rename) under one advisory flock with
// a timeout (exit 7 "state busy"). A corrupt cursor file is quarantined and
// treated as lost (fail open, bounded re-read); a corrupt ledger blocks sends
// (fail closed) because silently resetting it would hide duplicates.
package state
