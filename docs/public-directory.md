# Public featured and recent sites

The landing page has two separate sources. `featuredSites` in `internal/server/directory.go` is a small, explicit list of curated public domains (`sire.run`, `zati.agency`). These links visit the named websites; they do not represent measured results or claim an endorsement. Adding a featured domain never creates a scan, score, or report.

Recent reports use a bounded projection in the existing store record `public_directory_v1`, containing at most 24 domain/immutable-report-ID/completion-time/expiry entries. Each successful publication attempts an idempotent transactional update after the immutable `report_` record and domain pointer exist. It retains the newest entry per domain, removes expired candidates, sorts by completion time with a deterministic ID tie-break, and caps the stored window. The update callback resets its candidate on transaction retries. Older/replayed publications cannot displace newer entries.

This is a best-effort directory, not a second publication authority or scheduler. A projection-write failure logs a generic warning and does not block scan completion, HTML/PDF access, or terminal queue state. Existing publication recovery may repeat the projection update. There is no automatic backfill of historical reports and no guarantee every completion appears if a directory write failed. The empty-state copy therefore says no recent reports are *listed*, not that no scans have occurred. A future backfill would need a separately bounded design; do not scan all history at startup or on a page request.

For each landing-page request:

- Read this one projection document, take at most 24 candidates, and sort them by completion time.
- Validate IDs and canonical domains; skip projection entries already expired.
- Read only matching published `report_{id}` records. Require matching ID/domain/completion time, nonzero and nonfuture completion, and current snapshot expiry. Missing/unpublished attempts and expired snapshots are excluded. Never read request, consent, sender, or suppression records.
- Return at most six distinct domains. Stop once six valid reports are found. Worst case: one projection read and 24 public report reads, with a shared two-second request deadline. No collection/history query or `Store.List` is used.
- On storage failure, render an unavailable directory while leaving the scan form available. Links use the canonical `/{domain}` route; a later scan can make that route newer than the listed observation, while each report page provides its immutable snapshot link.

## Coordinator integration

No `Store` interface change, new collection, composite Firestore index, startup configuration, infrastructure edit, cloud inspection, or deployment is needed for the projection itself. It is one additional application record in the existing `records` collection, written with the existing store transaction mechanism. The first qualifying publication creates it; an existing installation initially shows the empty state. Existing expiry enforcement applies at read time; projection updates also drop expired entries. No phone/contact data is retained in it.

Prepared fixtures cover bounded reads/results/storage, newest-first order, same-domain deduplication, expired/missing/unpublished/mismatched exclusion, no private-record access, empty/unavailable landing states, dedicated domain/immutable/PDF access with SMS off, and nonblocking projection-write failure. These fixtures are unrun under the current capacity refusal. Coordinator must run the final full required checks after fresh authorization/admission and independently verify store behavior on the actual deployment.
