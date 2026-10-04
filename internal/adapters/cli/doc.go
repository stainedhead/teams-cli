// Package cli is the command-line adapter: it parses arguments, calls the
// use-case facade (usecase.Commands) and renders exactly one envelope per run
// through agent-cli-core's output package. It owns no business rules and
// imports only usecase, domain and core packages; the composition root
// (cmd/teams) wires it to the real use cases.
package cli
