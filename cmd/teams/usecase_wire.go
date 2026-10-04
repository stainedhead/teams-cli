package main

import "github.com/stainedhead/teams-cli/internal/usecase"

// newCommands is the ONE call site of the use-case constructor.
func newCommands(d usecase.Deps) usecase.Commands { return usecase.New(d) }
