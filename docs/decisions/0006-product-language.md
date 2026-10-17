# Product labels and stored source identity

Date: October 16, 2026.

The owner asked to remove repetitive “fictional” wording throughout the product, including supplier, component and quote names. This request supersedes the original plan's repeated visible fictional labels; the imported plan and ZIP remain unchanged.

The frontend renders concise source names and descriptions for both existing and newly collected records. The default workspace is displayed as Presentation, provenance badges use concise workspace/source labels, and page copy no longer repeats the qualifier. Demo Controls remains in the profile section. Data Health continues to report that Lenovo is not configured; quote cost exclusions, recorded receipts and external placement semantics remain explicit.

Formatting applies at display boundaries. Stored origins, source aliases, record keys, quote identifiers, observations, history and copied quote snapshots remain intact. There is no database migration, reset, source rekeying or bulk rewrite. Supplier and order editors display cleaned legacy defaults while preserving the original stored name/description when the user changes another field. Notes and account identity are not passed through the source-label formatter.

The CSV template downloads as `internal-orders-template.csv`. Its contents retain exact supplier aliases and buyer/source tokens, so an unchanged template can still be validated and published. The import preview displays supplier names without altering the request body.

Validation uses the production frontend build and browser-only fixtures containing legacy source strings, snapshots and user-authored notes. Live accounts and data are not modified by browser QA. Deployment changes only the static frontend gateway.
