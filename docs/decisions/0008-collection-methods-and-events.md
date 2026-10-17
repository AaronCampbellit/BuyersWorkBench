# Collection methods and internal source events

Date: October 17, 2026.

This records the source-event foundation at migration 0009. [Decision 0009](0009-connection-management.md) extends it with multiple connection identities, buyer routing, preferences and local administration tooling; see [BUILD_STATUS.md](../BUILD_STATUS.md) for the current twelve-migration release evidence.

The supplier collection strategy requires interchangeable webhook, API polling, browser, public-page and email adapters. The current application already validates complete observations, stores history and computes purchasing conditions, but extraction and binding were hard-coded to the fictional connector. Status-only changes did not generate notifications.

`internal/collection` now owns an explicit immutable registry, fixed connector descriptors, a collector interface, and safe classified failures. Descriptors declare method, authentication-reference type and origin mode. Only the fictional adapter is registered. The worker resolves durable connection identity and the separately approved server-side scope, then calls the adapter outside the publication transaction. It revalidates the complete batch and current authorization, generation and lease before publishing. Adapter registration cannot enable a connection, expand its scope or choose its workspace/privacy classification.

`wb_changes` remains the canonical internal source-event log. Migration 0009 adds generated `order.<field>.changed` event types, event schema version 1 and append-only runtime permissions. Existing changes gain metadata without generating historical notifications. The history API also supplies run, connection and observation lineage. A baseline is an observation, not an invented change. Distinct accepted transitions each have their own event; unchanged observations have none.

The existing publication transaction now consumes status events into recipient-scoped in-app notifications. A notification refers to exactly one condition or source event, with a workspace/generation-bound foreign key. The unique event/recipient key and existing run/order/field identity protect repeated execution. If notification insertion fails, accepted observations, events and notices roll back together. The first consumer runs synchronously in this transaction; there is no asynchronous external-delivery outbox or exactly-once messaging claim.

Condition notification policy remains first activation, recurrence or material worsening. Ordinary source notifications initially cover status changes only. Current active workspace members receive them; generic observed orders do not have a modeled assigned buyer identity. Assignment-specific routing needs an explicit buyer-to-user mapping. The UI uses the notification's order reference to open evidence while preserving older condition-notification behavior.

The API additions are compatible response fields: notification `source_event_id`/`order_id`, empty `exception_id` for source notices, and typed event lineage in order history. Collection methods remain behind this API; replacing a verified extractor does not require frontend transport logic.

The database still permits one fictional connection per workspace and refuses private collection. Enabling real suppliers needs connection-scoped identity and completeness checks, source-specific configuration, credentials/session isolation and an approved source mapping. Sparse push/email evidence cannot claim complete-snapshot coverage; the first push integration should request a bounded authorized reconciliation. Inbound callbacks, real transports, richer inventory/pricing/shipment models and external notification delivery remain planned in the [collection strategy](../COLLECTION_STRATEGY.md).

Public Lenovo research is recorded separately from private account discovery. No account was accessed. Apply migrations using the one-off administration command before running matching API/worker binaries; runtime processes never migrate. Local verification established the source-event behavior; current deployment evidence is recorded separately in BUILD_STATUS.
