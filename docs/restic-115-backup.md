# Restic to 115 through OpenList

The production Synology image is published by manually running
`.github/workflows/test_docker.yml`. The manual job builds only the
`linux/amd64` base image and publishes both
`autofilm-openlist-restic:gateway` and an immutable commit SHA tag. Regular
main-branch and pull-request builds keep the upstream multi-platform test matrix.

Updated: 2026-08-08

## Architecture

Restic uses its native REST backend and sends encrypted repository objects to
OpenList. OpenList maps each named repository to a directory on an existing
storage mount. For a 115 mount, provider uploads continue through the existing
115 driver and account scheduler.

The integration does not use WebDAV, rclone, or an S3 compatibility endpoint.
Restic remains an unmodified upstream binary. The customized Backrest service
is the scheduler and recovery console.

## 115 upload initialization

Before a normal or multipart OSS transfer, the 115 driver sends an encrypted
upload-initialization request. The response supplies either a rapid-upload
match or the OSS upload parameters. This step does not transmit the file body.

Some provider responses can be malformed and fail ECDH/LZ4 decoding with an
error such as `slice bounds out of range`. OpenList retries only response
decryption and JSON parsing failures, at most three attempts with a new ECDH
session and token each time. The delays are 250 milliseconds. Network errors,
provider business errors, and request cancellation are returned immediately.
No Restic pack or OSS part is retransmitted by this retry.

## OpenList configuration

Add a `restic` block to `config.json`. Limits set to `0` are disabled.

```json
{
  "restic": {
    "enable": true,
    "username": "backrest",
    "password": "replace-with-a-long-random-password",
    "timezone": "Asia/Shanghai",
    "upload_mib_per_second": 4,
    "daily_upload_gib": 80,
    "monthly_upload_gib": 1500,
    "repositories": [
      {
        "name": "synology",
        "path": "/115/Backups/Synology",
        "upload_mib_per_second": 0,
        "daily_upload_gib": 0,
        "monthly_upload_gib": 0
      }
    ]
  }
}
```

The shared limits apply to all configured repositories. A non-zero repository
limit adds a narrower limit for that repository. Rate values use MiB/s; calendar
allowances use GiB.

Use a dedicated 115 backup directory. Existing movie directories are not valid
repository roots.

## Restic repository

```bash
export RESTIC_REST_USERNAME=backrest
export RESTIC_REST_PASSWORD='the-openlist-rest-password'
export RESTIC_PASSWORD_FILE=/run/secrets/restic-repository-password
export RESTIC_REPOSITORY=rest:https://openlist.example.com/restic/synology/

restic init
restic snapshots
```

The HTTP password protects the endpoint. The Restic repository password
encrypts backup contents and must be retained separately.

REST v2 list responses include object sizes. Data packs are stored under
`data/{first-two-characters}/{object-name}` so one 115 directory does not
accumulate every pack.

## Traffic accounting

The counter wraps the actual 115 OSS upload reader:

- a rapid-upload match uses no WAN quota;
- a normal upload records each byte read by the OSS client;
- a multipart retry records the retransmitted bytes again;
- local hashing and temporary-file caching do not consume the quota;
- usage is stored per repository and local calendar day.

At a daily or monthly limit, new provider reads return HTTP `429`. Backrest can
run the plan again after the next daily period. Objects that already completed
remain valid Restic objects; a later backup reuses indexed data and uploads only
missing content. Interrupted work may leave unreferenced packs, which a later
`prune` removes.

Administrator usage endpoint:

```text
GET /api/admin/restic/usage
Authorization: Bearer <OpenList administrator token>
```

The Backrest console reads the same data with the Restic HTTP credentials:

```text
GET /restic/_usage
Authorization: Basic <Restic HTTP credentials>
```

## Live Docker and Time Machine data

The remote repository is an off-site disaster copy. The local Hyper Backup and
Time Machine disks remain the primary source for routine restores.

Back up live mutable directories from a read-only Btrfs snapshot when the DSM
volume supports snapshots. This gives Restic a stable tree without stopping
Docker or Time Machine. It is crash-consistent rather than application-aware;
databases should also produce a logical dump or use their own snapshot hook.

Recommended source groups:

- exported DSM configuration, package inventory, certificates, scheduled-task
  definitions, and recovery notes;
- Compose files, container environment templates, secrets, image/version
  inventory, and Docker persistent volumes;
- custom services and their configuration;
- a read-only snapshot of the Time Machine shared folder for disaster recovery;
- selected user data that is not already replaceable from source control.

Exclude caches, logs, temporary files, Git working copies that are fully
published, media already stored elsewhere, Docker overlay data, and databases
covered by a logical dump. Put these rules in version-controlled Restic exclude
files and assign one Backrest plan per consistency group.

## Recovery

One file or directory can be located by snapshot and restored without restoring
the whole NAS:

```bash
restic find '*compose*.yaml'
restic ls latest /volume1/docker
restic restore latest --include /volume1/docker/home-assistant/configuration.yaml --target /restore
```

For another NAS platform, restore portable Docker assets and data first, then
recreate host-specific mounts, users, permissions, reverse proxy, certificates,
and scheduled tasks from the recovery notes. DSM configuration exports are for
DSM recovery; portable service configuration remains usable on other Linux NAS
systems.

Run `restic check` periodically. Schedule `forget` and `prune` separately from
normal backups because maintenance performs broader repository reads and object
deletions.
