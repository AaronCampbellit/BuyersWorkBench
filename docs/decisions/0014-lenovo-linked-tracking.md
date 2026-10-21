# 0014: Order-linked carrier tracking

Date: October 20, 2026. The owner requested VM inspection of Lenovo's tracking links and collection as Linked tracking information on the matching order.

Extend the standalone extraction document with `Shipment.trackingLink` and `Order.linkedTrackingInformation`. Links retain exact line/shipment keys, tracking number, carrier and `lenovo_order_detail` provenance within their enclosing order. Stable identities remain independent of ETA, URL presentation and array position. Do not join by product description or chronological proximity.

Review the observed UPS legacy host/path and query keys explicitly. Require a source link and a package number that matches the same shipment, validate the carrier, reject unsafe or unreviewed URLs, then construct a minimal HTTPS URL. Preserve explicit absent/invalid/unreviewed/mismatched states without retaining rejected URLs or their query values. Do not fabricate links for missing source fields. A missing previously accepted reviewed link fails completeness rather than silently removing access to known tracking evidence.

Version 0.2.0 remains an additive `lenovo_orders.v1` extraction. Existing 0.1.0 encrypted captures and digests stay readable. Capturing a newly supported field is coverage enrichment and creates no retroactive supplier-change events. Later link changes participate in ordinary comparisons. Capture metadata includes a link count without exposing identifiers or URLs in logs.

Follow carrier links only in separately scoped inspection. The tested UPS context started without Lenovo cookies, used reviewed HTTPS hosts and source-read operations, and did not submit delivery changes, passwords or CAPTCHA responses. A normal site POST returned matching tracking details in one test; another bounded attempt did not return details. Consequently the production collector only extracts links from already validated Lenovo JSON and adds no UPS network dependency or host permissions. Carrier status/ETA enrichment remains separate work. Lenovo status, carrier status and buyer receipt must retain distinct origins and meanings.

Application ingestion and its private Orders view remain unimplemented for Lenovo. This change supplies the order-linked contract and private capture, not deployed UI publication. Owner-local readable exports remain outside the checkout; no real tracking URL, identifier, address or signature belongs in public source or fixtures. See BUILD_STATUS for executed checks.
