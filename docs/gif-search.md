# External @gif search

The inline `@gif` bot answers `messages.getInlineBotResults` for the GIF tab in
every Telegram client. By default it searches only the local curated catalogue
seeded from `TELESRV_GIF_SEED_DIR` (at most 50 entries), which is enough for a
small deployment but is not an internet search.

`TELESRV_GIF_PROVIDER` adds a real search backend. When it is enabled and the
query is non-empty, the bot asks the provider, downloads the hits, stores them as
canonical silent H.264 GIFv documents and returns them as ordinary inline
results. The local catalogue stays the fallback: an empty query, a provider
error, or a run where nothing could be downloaded all continue to the curated
catalogue instead of failing the inline query.

## Providers

| Provider | Value | Key | Notes |
|---|---|---|---|
| Klipy | `klipy` | app key from <https://klipy.com> | Tenor-compatible replacement with MP4 renditions; documented at <https://docs.klipy.com/gifs-api/gifs-search-api> |
| Giphy | `giphy` | beta key from <https://developers.giphy.com/dashboard/> | Beta keys are limited to 100 requests per hour; production keys are a paid upgrade |

Tenor is not an option: Google shut its public API down on 2026-06-30 and stopped
issuing new keys on 2026-01-13.

## Configuration

```dotenv
TELESRV_GIF_PROVIDER=klipy
TELESRV_GIF_API_KEY=<key>
TELESRV_GIF_SEARCH_LIMIT=12
TELESRV_GIF_CACHE_TTL=24h
```

When the provider is not `off`, the key is required and startup fails without it.
`TELESRV_GIF_SEARCH_LIMIT` is bounded by the protocol ceiling of 50 inline
results; keep it small, because every result is downloaded, transcoded by ffmpeg
and stored in the blob backend.

## Cost and behaviour

- One query mints at most `TELESRV_GIF_SEARCH_LIMIT` documents, downloading each
  in parallel (four at a time) through the external-media client, so the existing
  size cap (`TELESRV_EXTERNAL_MEDIA_MAX_BYTES`, 10 MiB by default), SSRF block
  list and per-minute fetch budget (`TELESRV_EXTERNAL_MEDIA_RATE_PER_MIN`) apply.
- Minted documents are real GIFv documents: they survive restarts, can be
  forwarded, saved and re-sent like any other GIF.
- Successful result sets are cached per normalized query for
  `TELESRV_GIF_CACHE_TTL`; a cache hit costs no provider request and no download.
  `0` disables the cache.
- Provider errors are logged by the caller as a fallback to the local catalogue,
  not as an inline-query failure.

## Tests

`go test ./internal/app/gifsearch ./internal/app/bots` covers the provider
response parsing, the mint cache and the bot's search-then-catalogue fallback.
