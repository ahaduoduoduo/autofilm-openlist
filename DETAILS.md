# Custom OpenList module map

Updated: 2026-08-11

The upstream OpenList structure is unchanged. Custom responsibilities are kept
in small modules.

## Restic backup gateway

- `server/restic/handler.go`: Restic REST v1/v2 protocol, repository mapping,
  basic authentication, immutable object reads and writes, Range responses,
  and two-character sharding for data packs.
- `server/restic.go`: `/restic/{repository}/` registration on the main OpenList
  HTTP service.
- `internal/resticquota/quota.go`: provider-upload byte accounting, shared and
  per-repository rates, daily/monthly calendar limits, and usage snapshots.
- `internal/model/restic_traffic_usage.go` and
  `internal/db/restic_traffic_usage.go`: one durable actual-upload counter per
  repository and day.
- `internal/model/restic_repository_object.go` and
  `internal/db/restic_repository_object.go`: persistent sizes of committed
  Restic objects, used to report current repository occupancy without
  repeatedly listing 115.
- `drivers/115/util.go`: retries malformed encrypted upload-initialization
  responses with fresh ECDH sessions before OSS transfer, and wraps only the
  115 OSS body readers associated with a Restic request. Local hashing and 115
  rapid-upload checks are not counted.
- `drivers/115/driver.go`: selects ordinary OSS upload for Restic objects up to
  64 MiB by reading the Restic request context. All non-Restic uploads retain
  the original 10 MiB ordinary-upload threshold.
- `drivers/115/driver_test.go`: verifies the separate Restic and general
  upload thresholds without contacting 115.
- `drivers/115/util_test.go`: upload-initialization retry, exhaustion,
  non-decode error, and cancellation coverage.
- `server/handles/restic.go`: administrator-only usage response consumed by the
  customized backup console.
- `docs/restic-115-backup.md`: deployment, repository, quota, occupancy
  inventory, and recovery reference.

## AutoFilm integration

- `internal/driver/autofilm.go`: provider-neutral authentication, passive
  authentication state, and scheduler contracts.
- `drivers/115/scheduler.go`: per-account HTTP rate limiting and operation
  concurrency.
- `drivers/115/auth.go`: expiring QR sessions, HTTP 405 state recording, and
  explicit reauthentication state for missing credentials; credentials remain
  inside OpenList, and reading state never calls 115.
- `server/handles/autofilm.go`: authenticated object, exact same-storage move,
  deletion, QR, scheduler, in-memory offline-task snapshot, and task-scoped
  provider cancellation;
  standard directory and
  offline-download handlers are mounted only on explicit integration routes;
  virtual mount responses are generated locally without provider requests.
  Refreshed object lookup reloads only the object's parent directory and
  resolves the target from that provider result.
  Exact moves execute synchronously through the selected driver, optionally
  rename in the destination after the move, reject destination collisions, and
  return the final object so Core can persist the actual path. Moving before
  renaming avoids transient post-rename lookup failures on remote drivers; a
  bounded 7-second visibility wait precedes the destination rename.
- `server/handles/autofilm_jellyfin.go`: explicit administrator-requested file
  or directory import in Jellyfin. It validates and forwards additive or full
  scan mode; OpenList filesystem mutations never call it.
- `server/handles/autofilm_test.go`: virtual directory response coverage.
- `server/middlewares/autofilm.go`: constant-time validation for the dedicated
  AutoFilm service token; its temporary user context exists only inside the
  integration route group.
- `server/router.go`: least-privilege `/api/autofilm` route registration and the
  administrator-compatible `/api/admin/autofilm` alias.
- `internal/offline_download/tool/download.go`: idempotent provider-side task
  removal shared by normal cancellation and AutoFilm short-deadline retries;
  it also retains the provider's task ID, acceptance time, and final result name
  in task memory.
- `server/handles/task.go`: adds `provider_task_id` and
  `provider_submitted_at` after provider acceptance, plus `result_path` after
  completion. The provider task ID controls retry timing only and is not a
  persistent media object ID.
- `Dockerfile.autofilm`: builds the modified OpenList binary together with the
  AutoFilm OpenList frontend. Deployment images check out the frontend `main`
  branch explicitly, so backend branch names do not change the embedded UI.

The AutoFilm protocol and failure semantics are documented in
`docs/autofilm-remote-api.md`.
