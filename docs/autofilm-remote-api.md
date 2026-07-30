# AutoFilm remote API

Updated: 2026-07-30

## Purpose and trust boundary

Jellyfin identifies remote files by absolute OpenList paths such as
`/115/movie/title.mkv`. The public integration contract does not expose or
require an OpenList storage ID, provider object ID, pick code, or 115 cookie.
OpenList does not maintain a second object catalog for Jellyfin.

Set `AUTOFILM_SERVICE_TOKEN` to a random value of at least 32 characters.
Requests send the value as the raw `Authorization` header or as a Bearer token.
The token is accepted only by the AutoFilm route group and cannot authenticate
against regular filesystem or administrator routes.

Base path: `/api/autofilm`

The administrator-authenticated `/api/admin/autofilm` alias remains available
for diagnostics. Jellyfin uses only the service-token path.

## Core task API

`POST /directories` creates one destination directory using the standard
OpenList path permission checks.

`POST /offline-downloads` creates offline download tasks using the standard
OpenList task manager. The request fields are `urls`, `path`, `tool`, and
`delete_policy`; the response contains the created task IDs. These are the only
task-creation operations exposed to the dedicated service token.

## Path object API

### Get one object

`POST /objects/get`

```json
{"path":"/115/movie/title.mkv","refresh":false}
```

The response contains provider-neutral data:

```json
{
  "path": "/115/movie/title.mkv",
  "name": "title.mkv",
  "size": 123456,
  "is_dir": false,
  "modified": "2026-07-28T00:00:00Z",
  "created": "2026-07-28T00:00:00Z",
  "download_path": "/d/115/movie/title.mkv?sign=..."
}
```

`download_path` is omitted for directories. Jellyfin combines it with the
configured public OpenList URL for client playback and with the internal
OpenList URL for serialized `ffprobe`.

`refresh:true` refreshes only the object's provider-backed parent directory and
resolves the object from that new listing. Jellyfin uses this after an offline
task reaches its succeeded state so a stale OpenList directory cache cannot
hide the newly created result. Normal playback and subtitle reads leave it
disabled.

### List one directory

`POST /objects/list` accepts `/` and intermediate OpenList virtual directories.
Those responses are built from configured mount paths and do not call storage
providers. This allows Jellyfin's directory picker to start at the OpenList
root without producing a burst of 115 requests.

`POST /objects/list`

```json
{"path":"/115/movie","refresh":false}
```

The response contains `objects`. `refresh:true` requests a provider refresh and
must be used selectively because it may consume a 115 API request.

### Upload one file

`PUT /objects/put` uses the standard OpenList streaming-upload request:

- `File-Path`: percent-encoded absolute destination path
- `As-Task`: `false` for serialized subtitle writes
- `Overwrite`: normally `false`
- request body: file bytes

AutoFilm uses this route for new subtitles and one-at-a-time lazy migration of
legacy subtitles.

### Delete one path

`POST /objects/delete`

```json
{"path":"/115/movie/title.mkv"}
```

Storage-root deletion is rejected. A successful request deletes the path
through the selected OpenList driver. Jellyfin calls this operation before
removing its own item, so a remote deletion failure preserves the Jellyfin
record.

## Explicit Jellyfin scan

OpenList file mutations do not imply a Jellyfin media-library change. Upload,
move, rename, copy, and delete operations therefore do not create AutoFilm
events or call Jellyfin.

`POST /jellyfin/scan` is the explicit exception. It is used only when an
administrator intentionally wants Jellyfin to import or refresh an OpenList
path:

```json
{
  "path": "/115/movie/title.mkv",
  "refresh": false,
  "recursive": false,
  "force_probe": true
}
```

Configure this operation with:

- `AUTOFILM_JELLYFIN_URL`, for example `http://jellyfin:8096`
- `AUTOFILM_JELLYFIN_API_KEY`, a Jellyfin administrator API key

The handler forwards the request to Jellyfin `POST /AutoFilm/RemoteRefresh`.
It does not list the OpenList directory itself. Jellyfin performs the bounded
path lookup through the object API above.

AutoFilm Core normally calls Jellyfin directly after a download completes; it
does not route routine refreshes through this OpenList endpoint.

## 115 authentication and scheduling

The following endpoints are OpenList administration operations, so they use an
internal storage selection value; that value is not part of Jellyfin's media
model:

- `POST /auth-sessions` starts or reuses an unexpired QR session.
- `GET /auth-sessions/status` returns only session state and expiry.
- `GET /auth-sessions/qrcode.png` returns the QR image.
- `GET /auth-state?storage_id=...` returns the last known authentication state
  stored by OpenList. It never contacts 115.
- `GET /auth-health?storage_id=...` is a compatibility alias for the same
  passive state and also never contacts 115.
- `GET /scheduler` returns non-sensitive request and concurrency counters.

The driver records a risk-control marker only when a real 115 operation returns
HTTP 405. It does not run a periodic Cookie check. A confirmed QR login replaces
the Cookie and clears the marker. A later successful response from the normal
115 client also clears the marker, so temporary provider recovery does not
require a forced login.

`GET /offline-tasks` returns the current in-memory download and transfer task
snapshot. It does not list storage objects or call 115. AutoFilm Core uses this
endpoint for progress updates. For a completed 115 download, `result_path`
contains the destination directory joined with the final name reported by 115.
It is omitted until the provider has returned that name. No provider object ID
is exposed.

`POST /offline-tasks/delete` accepts `{"task_id":"..."}` for a download task
created through `/offline-downloads`. It cancels the OpenList task and removes
the corresponding provider-side offline task without deleting any destination
object. AutoFilm Core uses it before trying the next release candidate when a
115 instant offline transfer exceeds its configured short deadline.

The 115 cookie stays inside its driver. Every 115 request passes through the
per-account limiter. Defaults are one request per second, burst one, and one
concurrent list, mutation, or upload. Bulk Jellyfin path migration performs no
OpenList request; new media probes and lazy subtitle uploads remain serialized.
