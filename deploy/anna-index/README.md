# Anna local index sidecar

This stack keeps the heavy Anna metadata catalogue in PostgreSQL and exposes it to LindasLibrarr over a loopback-only HTTP API. It does not replace LindasLibrarr's existing SQLite database for application state.

## Upstream source

The sidecar is built from `iziplay/anna-api`. For reproducible NAS deployment, check out the reviewed upstream commit before building:

`e9624db7b5e685b66151cb1810cdcdbd1e8dbdff`

Do not track upstream `main` blindly in the live stack.

## Search path

When LindasLibrarr has:

`ANNA_LOCAL_API_URL=http://127.0.0.1:5051`

it uses the local PostgreSQL-backed API for Anna ebook search and does not also launch the public Anna HTML `/search` request. Results keep source name `annas`, so the existing Anna membership download flow can use the result MD5.

If `ANNA_LOCAL_API_URL` is absent, LindasLibrarr retains the existing HTML search provider for backwards compatibility.

## Update behavior

`anna-api` checks Anna's torrent metadata catalogue and performs its own metadata synchronization. Its application loop uses a 24-hour interval. The torrent payload used for metadata ingestion is removed after a successful sync because `ANNA_KEEP_FILES=false`; PostgreSQL remains persistent.

The first full synchronization is intentionally not part of deployment smoke testing. Before starting it:

1. Verify `/dyn/torrents.json` is reachable from the NAS.
2. Bring up PostgreSQL and the API with a single `ANNA_ARCHIVE_ID` shard for a small import test.
3. Verify local search latency, result mapping, MD5s, covers, languages, and database persistence.
4. Remove `ANNA_ARCHIVE_ID` and perform the full initial sync only after the smoke test is green.

## Persistent data

- PostgreSQL: `/volume1/WDBLACK/ContainerConfigs/AnnaIndex/postgres`
- temporary torrent metadata: `/volume1/WDBLACK/ContainerConfigs/AnnaIndex/torrents`

The database password must be supplied to the stack as `ANNA_DB_PASSWORD`; never commit it to Git.
