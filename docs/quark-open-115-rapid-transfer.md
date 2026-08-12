# QuarkOpen authentication and 115 rapid transfer

Updated: 2026-08-12

## Purpose

The QuarkOpen driver can use the refresh token produced by the OpenList token
page with server-provided application parameters. The same driver exposes
validated Quark file SHA-1 metadata to OpenList's cross-storage copy operation,
allowing a 115 destination to attempt provider-side rapid upload.

Rapid upload is conditional. It succeeds only when 115 accepts the file size
and SHA-1 as existing content for the destination account. It is not a direct
cloud-to-cloud copy between Quark and 115.

## Configuration

1. Open `https://api.oplist.org/`, select Quark and the option to use OpenList
   server-provided parameters, then complete QR authorization.
2. Copy the returned refresh token into the QuarkOpen storage configuration.
3. Leave Access Token, AppID, and SignKey empty. Their fields are optional.
4. Save the storage. The default online refresh setting and API address may
   remain unchanged.

During initialization, an absent access token, AppID, or SignKey causes the
driver to send the refresh token to the Quark OAuth issuer at
`https://oauth.fnnas.com/api/v1/oauth/refreshToken`. The response must include:

- access token;
- rotated refresh token;
- AppID used as `x-pan-client-id`;
- SignKey used to generate `x-pan-token`.

The driver saves all four values together before requesting Quark user data.
Once AppID and SignKey exist, later access-token refreshes retain the configured
OpenList online refresh service for compatibility. Refresh operations are
serialized so one storage instance does not rotate the same token
concurrently. Token values are not written to logs.

An invalid or expired refresh token still prevents the storage from starting.
Generate a new refresh token through the token page in that case.

## SHA-1 mapping

Quark's file-list response can contain `content_hash` and optionally
`content_hash_name`. The driver publishes that value as SHA-1 only when all of
the following are true:

- the algorithm name is empty, `sha1`, or `sha-1`, ignoring case;
- the value contains exactly 40 characters;
- every character is hexadecimal.

Uppercase hashes are normalized to lowercase. MD5-length values, explicitly
different algorithms, malformed values, and missing values are not published
as SHA-1.

## Copy behavior

For a QuarkOpen source and 115 destination, OpenList carries the source object
SHA-1 into the upload stream. The 115 driver reads that SHA-1 during upload
initialization. When 115 reports a rapid-upload match, OpenList does not need to
read the complete Quark source file and no file body is transmitted to 115.

When no validated SHA-1 is available or 115 rejects the rapid-upload check,
the existing streamed-copy path remains active. This fallback may download the
source through OpenList and upload it to 115, so the presence of a hash improves
the opportunity for rapid upload but does not guarantee it.

## Operational boundary

The code change does not alter a running container, Compose configuration, or
mounted storage automatically. Deploying a newly built image and restarting
OpenList are separate administrator actions. Because refresh tokens can rotate,
do not test a production refresh token against the issuer outside the driver
while that storage is active.
