# Issue resolution and verification

Date: October 16, 2026.

The owner requested a complete closeout flow after finding that a task status and separately saved notes did not explain how to resolve a purchasing issue.

An issue has a human workflow and a separately calculated source condition. A single Resolve issue submission records resolution type, action taken, result, supporting reference, optional supplier contact, optional revised date and confirmed quantity, and a verification follow-up date when source risk remains active. It records the submitting person/time and a snapshot of the condition being addressed in the same transaction as the status change. Claimed dates and quantities are buyer evidence; they do not overwrite supplier observations, internal receipts or quote/order terms.

An active condition enters `awaiting_verification`. A cleared condition can enter `resolved` with a server timestamp. Accepted source evidence clearing a waiting condition verifies it automatically and adds a source event. Recurrence and worsening of waiting or resolved work reopen it and preserve previous closeout details. A user can explicitly reopen with a required reason. Failed, incomplete or missing observations cannot establish resolution.

The priority queue defaults to work requiring action, including old `completed` records that lack structured closeout details. Waiting and resolved work have distinct filters; risk remains separately filterable. New generic workflow updates cannot bypass the closeout or reopening actions. Legacy task completions and all existing notes remain intact.

An append-only, workspace/generation-scoped event table records submissions, workflow changes, source verification and reopening. Runtime access is limited to scoped reads/inserts, with accountable user actors or explicitly labeled automatic source events. Generation/revision checks and idempotency protect retries, concurrent edits and reset boundaries. Closeout fields and unsaved drafts remain visible after rejected or stale requests; users explicitly review current state before applying a stale draft.

This change does not enable live Lenovo collection, supplier messages, attachment upload or receipt posting. References are plain text identifiers for supporting records. Live source discovery remains deferred as previously requested.
