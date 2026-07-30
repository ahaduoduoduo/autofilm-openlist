# AutoFilm module map

The upstream OpenList structure is unchanged. AutoFilm-specific responsibilities
are kept in small modules:

- `internal/driver/autofilm.go`: provider-neutral authentication, credential
  health, and scheduler contracts.
- `drivers/115/scheduler.go`: per-account HTTP rate limiting and operation
  concurrency.
- `drivers/115/auth.go`: expiring QR sessions and explicit Cookie health checks;
  credentials remain inside OpenList.
- `server/handles/autofilm.go`: authenticated object, deletion, QR, scheduler,
  in-memory offline-task snapshot, and task-scoped provider cancellation;
  standard directory and
  offline-download handlers are mounted only on explicit integration routes;
  virtual mount responses are generated locally without provider requests.
- `server/handles/autofilm_jellyfin.go`: explicit administrator-requested path
  import or refresh in Jellyfin. OpenList filesystem mutations never call it.
- `server/handles/autofilm_test.go`: virtual directory response coverage.
- `server/middlewares/autofilm.go`: constant-time validation for the dedicated
  AutoFilm service token; its temporary user context exists only inside the
  integration route group.
- `server/router.go`: least-privilege `/api/autofilm` route registration and the
  administrator-compatible `/api/admin/autofilm` alias.
- `internal/offline_download/tool/download.go`: idempotent provider-side task
  removal shared by normal cancellation and AutoFilm short-deadline retries.
- `Dockerfile.autofilm`: builds the modified OpenList binary for the isolated
  integration image.

The wire protocol and failure semantics are documented in
`docs/autofilm-remote-api.md`.
