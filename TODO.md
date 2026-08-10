# Custom OpenList development status

Updated: 2026-08-11

## Completed

- [x] 2026-08-11: Add persistent task-level upload allocations and weighted
  sharing of the existing 115 upload-concurrency pool; completed tasks release
  unused allocation without changing the global calendar quota.
- [x] 2026-08-11: Route only Restic uploads up to 64 MiB through ordinary OSS
  PutObject, while preserving the existing 10 MiB threshold for every other
  OpenList upload.
- [x] 2026-08-10: Track committed Restic object sizes in a persistent local
  inventory, expose current repository occupancy in the usage API, and add an
  authenticated local-index seed operation for repositories that already
  exist without enumerating remote 115 data shards.
- [x] 2026-08-09: Keep upstream beta image publication available only to the
  OpenListTeam repository; this fork uses its Test Build workflow for pull
  requests and does not build the unused `latest`, `ffmpeg`, `aria2`, and `aio`
  image matrices.
- [x] 2026-08-09: Run 115 driver tests in the pull-request test workflow and
  keep the replaced-client test independent from initialized server config.
- [x] 2026-08-09: Rebuild the 115 client after QR authentication and validate
  fresh upload identity metadata before persisting the credential, preventing
  read recovery from leaving uploads on the pre-scan `UserID`/`UserKey` pair;
  expose persisted `bad cookie` and `user not login` states as requiring QR
  authentication after a restart.
- [x] 2026-08-09: Make every fork build default to the AutoFilm OpenList
  frontend and make the amd64/restic deployment image build both repositories
  from source instead of silently falling back to the upstream UI.
- [x] 2026-08-09: Expose missing 115 credentials as a passive
  `requires_reauthentication` state so Core can distribute a QR code after a
  risk-control restart without probing 115.
- [x] 2026-08-08: Retry malformed encrypted 115 upload-initialization
  responses with a fresh ECDH session before any OSS file data is transmitted.
- [x] 2026-08-06: Add a manual Synology deployment build that publishes one
  linux/amd64 image with mutable gateway and immutable commit tags.
- [x] 2026-08-06: Add a native Restic REST v1/v2 endpoint backed by mapped
  OpenList directories, including Range reads, data-pack sharding, HTTP basic
  authentication, and an administrator traffic-usage API.
- [x] 2026-08-06: Apply Restic-specific rate, daily, and monthly limits at the
  actual 115 OSS reader. Rapid-upload matches consume no WAN quota, while
  multipart retries consume their actual transmitted bytes.
- [x] 2026-08-06: Extend the explicit Jellyfin scan request with validated
  `new` and `full` modes for both OpenList video files and directories.

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

- [ ] Validate Restic init, backup, snapshots, restore, check, and prune against
  a dedicated 115 test directory through the GitHub-built image.
- [ ] Display `/api/admin/restic/usage` in the customized Backrest console.
- [ ] Add metrics for provider latency, throttling, and authentication failures.
- [ ] Verify QR login and upload behavior against a dedicated 115 test account.
