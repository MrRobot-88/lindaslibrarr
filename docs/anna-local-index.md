# Anna local index architecture

This branch introduces the data model for a local Anna availability index without changing the live Anna HTML search path yet.

## Separation of concerns

- `catalog_books`: canonical works shown to clients.
- `catalog_editions`: ISBN/language/publisher editions.
- `catalog_releases`: acquisition availability from Anna/Prowlarr.
- `catalog_library_files`: shared physical library. Profiles do not own duplicate copies.
- `catalog_profiles`: independent readers.
- reactions/progress/favorites/recommendations are profile scoped.
- `catalog_sync_state`: tracks background source synchronization.

A reaction is `1` for like and `-1` for dislike. It never removes a shared library file.

## Anna ingestion

The intended importer follows the metadata-torrent model rather than scraping Anna HTML search. It will ingest Anna metadata in the background, upsert releases, retain language information and use identifiers such as ISBN/MD5 for matching.

Preferred automatic release language order is `sv`, `en`, `da`, `es`; other indexed languages remain selectable.

The synchronization target is once every 24 hours. A sync checks for a new metadata base first and does no full rebuild when the current base is already imported.

## Rollout safety

The existing Anna HTML provider is intentionally not replaced in this first migration commit. The local index must pass importer/search tests before it is wired into the live search endpoint.
