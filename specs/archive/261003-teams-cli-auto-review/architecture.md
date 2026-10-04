# Architecture: teams CLI Auto-Review Fixes
Date: 2026-10-03 | Status: Draft

## Architecture Overview
Existing Clean Architecture is unchanged; fixes stay within current layers and archtest rules.
## Component Architecture
## Layer Responsibilities
Config resolves precedence (policy over env); usecase owns reserve-then-post and audit-intent ordering; adapters present output.
## Data Flow
## Sequence Diagrams
## Integration Points
Graph messages API, audit sink, state ledger.
## Architectural Decisions
(record as made)
