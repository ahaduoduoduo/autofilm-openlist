# AutoFilm development status

Updated: 2026-07-31

## Completed

- [x] 2026-07-31: Expose provider task ID and provider acceptance time only
  after the offline driver accepts a URL, keeping OpenList queue time outside
  AutoFilm Core's short 115 completion deadline.
- [x] 2026-07-31: Move AutoFilm objects under their original name before an
  optional destination rename, eliminating the remote post-rename lookup that
  could leave an upgrade file renamed inside its staging directory.
- [x] 2026-07-31: Add an exact synchronous same-storage object move to the
  least-privilege AutoFilm API. It rejects roots, cross-storage moves and
  destination collisions, supports a final file name, and returns the final
  provider-neutral path for media replacement and old-file backup.
- [x] 2026-07-30: Make refreshed object lookup reload the exact parent
  directory so completed offline-download results do not require a manual
  OpenList browser refresh before Jellyfin import.
- [x] 2026-07-30: Expose the final 115 offline-download result path so
  AutoFilm Core can import the exact object into Jellyfin instead of refreshing
  a month-level parent directory.
- [x] 2026-07-28: Reduce the Jellyfin-facing object contract to absolute paths;
  storage IDs and provider object IDs remain internal.
- [x] 2026-07-28: Add path lookup, bounded directory listing, direct upload,
  signed download, and path deletion APIs.
- [x] 2026-07-28: Expose virtual mount directories through the AutoFilm listing
  API without querying cloud providers.
- [x] 2026-07-29: Remove global object observations and durable filesystem
  events. OpenList file mutations no longer imply Jellyfin library changes.
- [x] 2026-07-29: Add an explicit path-based Jellyfin scan operation for
  administrator-requested imports and refreshes.
- [x] 2026-07-27: Add channel-neutral 115 QR login sessions.
- [x] 2026-07-27: Add 115 account-level rate and concurrency scheduling.
- [x] 2026-07-27: Add a dedicated, least-privilege AutoFilm service token and
  direct subtitle upload route.
- [x] 2026-07-28: Expose a least-privilege, read-only in-memory offline-task
  snapshot for AutoFilm Core progress tracking.
- [x] 2026-07-29: Add a task-scoped cancellation endpoint that removes the
  provider-side 115 offline task without deleting destination objects.
- [x] 2026-07-30: Record real 115 HTTP 405 responses as a persistent,
  machine-readable risk-control state. State reads do not contact 115; QR
  authentication or a later successful provider request clears the marker.

## Planned

- [ ] Add metrics for provider latency, throttling, and authentication failures.
- [ ] Verify QR login and upload behavior against a dedicated 115 test account.
