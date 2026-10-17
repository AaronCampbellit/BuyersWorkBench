# Fictional pipeline and backend build

October 16, 2026. The owner authorized continuing through the milestone backlog after tunnel setup, then explicitly deferred live Lenovo discovery to a later browser investigation while completing the backend.

The implementation keeps one Go module and PostgreSQL transaction boundary. A small `internal/domain` package owns normalized contracts, exact decimal calculations, rules, and owned copies of the fictional fixtures. `internal/workbench` owns scope, durable source revisions, staged/quarantined batches, publication, human workflow, scenarios, schedules, generation fences and reset. HTTP routes derive actor identity from the session; source payloads cannot select their workspace/privacy class.

Domain records use bounded JSONB for the narrow normalized model alongside relational workspace/generation keys, composite foreign keys, unique transition/event identities and indexed current-state/run fields. Transaction-local verified actor context plus runtime-role RLS protects workspace rows and current generations. Human workflow persists separately from source facts. Immutable prior generations remain hidden from current-generation reads instead of destructive reset deletion; security audit and private history are preserved.

The fictional connector performs collection outside the short publication transaction. Each connection has a lease/fence; publication reacquires the workspace lock and verifies generation, actor authorization, lease, binding, identity and completeness. A successful publication and notification insertion are transactional; repeated runs do not recreate transitions/material conditions. Reset exposes an explicit initializing state until a validated new-generation baseline is accepted. Persisted due schedules reconcile at startup/ticks; a basic in-memory River periodic timer is not treated as durable scheduling.

Sanitization is an explicit owner preview/approval operation using typed allowlisted output, independent public IDs, generic owner-reviewed descriptions, and private lineage stored separately. Snapshots remain separate from mutable fictional sources. CSV imports are fictional internal requirements with explicit current-source mappings and atomic preview consumption/publication.

The frontend retains the existing restrained desktop layout. It exposes only implemented views and displays source origin, date semantics, freshness, failure, modeled costs and coverage limits. No actual receipts means no on-time delivery metric; shipped quantity never becomes delivered quantity.

A live Lenovo adapter/runner is deliberately absent until actual permitted site behavior is investigated. Fictional success does not close source discovery, live collection, session secret handling or network egress acceptance gates.

Internal River collection arguments use an owned typed schema version1, distinct from the proposed generic planning job envelope. The normalized observation wire decoder follows the original observation fixtures. Fictional elapsed-hour rules currently evaluate at00:00UTC on the pinned business date; actual collection times remain UTC execution instants.
