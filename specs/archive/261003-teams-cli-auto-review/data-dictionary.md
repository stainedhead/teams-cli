# Data Dictionary: teams CLI Auto-Review Fixes
Date: 2026-10-03. Purpose: record data structures added or changed by the fixes.

## Entities
Ledger reservation (rate slot, pending/sent/failed) [FR-R4]
## Value Objects
Truncation indicator in skipped/audit extras [FR-R2]; link (untrusted) [FR-R6]
## Interfaces
Ledger Reserve/Release operation [FR-R4]; audit intent record [FR-R5]
## Enumerations
Audit outcome: delivered-not-audited [FR-R5]
## API Request/Response Types
whoami (destinations, limits, poll interval, policy path, version), destinations item (mentionable), dry-run (decision, destination, preview) [FR-R3]; AuditEvent.HTTPStatus [FR-R7]
