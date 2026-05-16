# jpzip-go

[![Go Reference](https://pkg.go.dev/badge/github.com/jpzip/go.svg)](https://pkg.go.dev/github.com/jpzip/go)
[![Go Report Card](https://goreportcard.com/badge/github.com/jpzip/go)](https://goreportcard.com/report/github.com/jpzip/go)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Test](https://github.com/jpzip/go/actions/workflows/test.yml/badge.svg)](https://github.com/jpzip/go/actions/workflows/test.yml)

> Go SDK for **jpzip** — a free, unlimited Japanese postal code (郵便番号) API.
> 日本の全郵便番号 120,677 件を CDN 配信 JSON から引く Go SDK。

**English** | [日本語](./README.ja.md)

`jpzip-go` looks up Japanese postal codes (郵便番号) from `jpzip.nadai.dev`,
a CDN-hosted dataset built from Japan Post's `KEN_ALL.csv` and `KEN_ALL_ROME.csv`
normalized to JSON. No registration, no rate limits, no API key.

- 🇯🇵 **Complete dataset** — 120,677 entries with kanji, kana, romaji, and government codes (JIS X 0401 / 総務省地方公共団体コード)
- ⚡️ **Fast** — L1 LRU + optional L2 persistent cache; `Preload` to serve lookups without per-request network round-trips
- 🛡️ **Resilient** — 3-attempt retry with exponential backoff on 5xx / network failures
- 🪶 **Zero deps** — standard library only
- 🆓 **Free forever** — backed by Cloudflare Pages' free tier (no billing axis exists)
- 🔌 **Drop-in** — same API surface across [every jpzip SDK](#other-languages)

## Requirements

Go 1.23+

## Install

```bash
go get github.com/jpzip/go
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"

    jpzip "github.com/jpzip/go"
)

func main() {
    entry, err := jpzip.Lookup(context.Background(), "2310017")
    if err != nil {
        panic(err)
    }
    if entry == nil {
        fmt.Println("not found")
        return
    }
    fmt.Println(entry.Prefecture, entry.City, entry.Towns[0].Town)
    // Output: 神奈川県 横浜市中区 港町
}
```

Romaji and government codes are included:

```go
fmt.Println(entry.PrefectureRoma, entry.CityRoma, entry.Towns[0].Roma)
// Output: Kanagawa Ken Yokohama Shi Naka Ku Minatocho

fmt.Println(entry.PrefectureCode, entry.CityCode)
// Output: 14 14104
```

## Use Cases

### Zipcode lookup HTTP endpoint (net/http)

```go
http.HandleFunc("/api/zipcode/", func(w http.ResponseWriter, r *http.Request) {
    code := strings.TrimPrefix(r.URL.Path, "/api/zipcode/")
    entry, err := jpzip.Lookup(r.Context(), code)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    if entry == nil {
        http.NotFound(w, r)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(entry)
})
```

### Zipcode lookup HTTP endpoint (chi)

```go
r.Get("/api/zipcode/{code}", func(w http.ResponseWriter, r *http.Request) {
    entry, err := jpzip.Lookup(r.Context(), chi.URLParam(r, "code"))
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    if entry == nil {
        http.NotFound(w, r)
        return
    }
    json.NewEncoder(w).Encode(entry)
})
```

### Batch validation

```go
all, err := jpzip.LookupAll(ctx) // entire dataset in memory (~37 MiB JSON)
if err != nil {
    log.Fatal(err)
}
for _, zip := range csvZipcodes {
    if _, ok := all[zip]; !ok {
        log.Printf("invalid zipcode: %s", zip)
    }
}
```

### Serve lookups from cache (BYO L2 backend)

The dataset is partitioned into 948 three-digit prefix buckets. The default
L1 (100 entries) keeps the hottest buckets; to cache the whole dataset, pair
`Preload("all")` with an L2 cache or raise `WithMemoryCacheSize` above 948.

```go
client := jpzip.New(
    jpzip.WithMemoryCacheSize(1024),
    jpzip.WithCache(myFileCache), // any Cache implementation
)
if err := client.Preload(ctx, "all"); err != nil {
    log.Fatal(err)
}
// Subsequent lookups are served from L1/L2 without hitting the network.
entry, _ := client.Lookup(ctx, "2310017")
```

## API Reference

Full docs on [pkg.go.dev](https://pkg.go.dev/github.com/jpzip/go).

### Functions (package-level, share a default Client)

| Function | Description |
|---|---|
| `Lookup(ctx, zipcode)` | Look up a single 7-digit zipcode. Returns `(nil, nil)` if not found or malformed (no network call for malformed input). |
| `LookupGroup(ctx, prefix)` | Look up by 1-, 2-, or 3-digit prefix. 1-digit fetches `/g/{d}.json`; 3-digit fetches `/p/{ddd}.json`; 2-digit fans out into 10 parallel 3-digit fetches and merges. |
| `LookupAll(ctx)` | Fetch entire dataset (120k entries, ~37 MiB) in parallel across `/g/0..9.json`. |
| `GetMeta(ctx)` | Dataset version, generated-at, per-prefecture counts, spec version. Result is cached until `Refresh`. |
| `Preload(ctx, scope)` | Warm L1 (and L2 when configured) for `"all"` or a specific prefix. |
| `IsValidZipcode(s)` | Pure syntax check (`^\d{7}$`) — no network. |

### Client (advanced)

`New()` returns a configurable instance; required for L2 caching, custom HTTP client, alternate base URL, or multiple isolated caches:

```go
client := jpzip.New(
    jpzip.WithBaseURL("https://jpzip.nadai.dev"),
    jpzip.WithHTTPClient(&http.Client{Timeout: 5 * time.Second}),
    jpzip.WithMemoryCacheSize(200), // L1 capacity in prefix buckets, default 100
    jpzip.WithCache(myCache),       // optional L2
    jpzip.OnSpecMismatch(func(expected, received string) {
        log.Printf("jpzip spec mismatch: SDK=%s server=%s", expected, received)
    }),
)
```

Client exposes `Lookup` / `LookupGroup` / `LookupAll` / `GetMeta` / `Preload` plus:

| Method | Description |
|---|---|
| `client.Refresh(ctx)` | Wipe L1 (and L2 when configured) and forget the cached meta. |

When `GetMeta` observes that `/meta.json`'s `version` has changed since the last successful fetch, L1 and L2 are cleared automatically — call `GetMeta` periodically to pick up dataset rollovers.

### Errors

- `ErrInvalidPrefix` — returned (wrapped) by `LookupGroup` / `Preload` when the prefix is not 1-3 digits. Match with `errors.Is(err, jpzip.ErrInvalidPrefix)`.
- Transient network failures and 5xx responses are retried up to 3 attempts (initial + 2 retries) with exponential backoff sleeps of 400ms and 800ms. 4xx responses (other than 404, which yields `(nil, nil)`) are returned immediately.

### `Cache` interface

Bring your own L2 backend (file, BoltDB, Redis, KV, etc.):

```go
type Cache interface {
    Get(ctx context.Context, key string) ([]byte, bool, error)
    Set(ctx context.Context, key string, value []byte) error
    Delete(ctx context.Context, key string) error
    Clear(ctx context.Context) error
}
```

Keys are the full prefix-bucket URLs (e.g. `https://jpzip.nadai.dev/p/231.json`); values are raw JSON bytes.

## Why jpzip-go?

| | **jpzip-go** | [oirik/gokenall][gokenall] | [syumai/go-jpostcode][jpostcode] | [zipcloud API][zipcloud] |
|---|---|---|---|---|
| Romaji (`Yokohama Shi`) | ✅ | ❌ | ❌ | ❌ |
| Government codes (JIS / 総務省) | ✅ | ⚠️ JIS only | ❌ | ❌ |
| No manual CSV download | ✅ | ❌ | ⚠️ Embedded | ✅ |
| Monthly updates | ✅ Auto | ❌ Manual | ❌ Manual | ✅ |
| Offline after preload | ✅ | ✅ | ✅ | ❌ |
| Rate-limit-free | ✅ | ✅ | ✅ | ⚠️ Discouraged |
| L1 + pluggable L2 cache | ✅ | ❌ | ❌ | ❌ |
| Zero dependencies | ✅ | ✅ | ✅ | n/a |

[gokenall]: https://github.com/oirik/gokenall
[jpostcode]: https://github.com/syumai/go-jpostcode
[zipcloud]: http://zipcloud.ibsnet.co.jp/doc/api

## Other Languages

Same API surface across all SDKs:

[TypeScript](https://github.com/jpzip/js) · [Python](https://github.com/jpzip/python) · [Rust](https://github.com/jpzip/rust) · [Ruby](https://github.com/jpzip/ruby) · [PHP](https://github.com/jpzip/php) · [Swift](https://github.com/jpzip/swift) · [Dart](https://github.com/jpzip/dart)

## Resources

- **Website** — https://jpzip.nadai.dev
- **Protocol spec** — [jpzip/spec](https://github.com/jpzip/spec)
- **Data ETL** — [jpzip/data](https://github.com/jpzip/data)
- **MCP server** — [jpzip/mcp](https://github.com/jpzip/mcp) — use jpzip from Claude / ChatGPT / Cursor

## Keywords

japanese postal code, japan zipcode, 郵便番号, KEN_ALL, KEN_ALL_ROME, address validation, address autocomplete, japan address api, postal code lookup go, golang japanese address, JIS X 0401, 総務省地方公共団体コード

## License

[MIT](./LICENSE)
