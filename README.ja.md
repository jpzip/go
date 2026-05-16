# jpzip-go

[![Go Reference](https://pkg.go.dev/badge/github.com/jpzip/go.svg)](https://pkg.go.dev/github.com/jpzip/go)
[![Go Report Card](https://goreportcard.com/badge/github.com/jpzip/go)](https://goreportcard.com/report/github.com/jpzip/go)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Test](https://github.com/jpzip/go/actions/workflows/test.yml/badge.svg)](https://github.com/jpzip/go/actions/workflows/test.yml)

> **jpzip** の Go SDK — 無料・無制限の日本郵便番号 API。
> 日本郵便の `KEN_ALL.csv` / `KEN_ALL_ROME.csv` を JSON 正規化し CDN 配信。

[English](./README.md) | **日本語**

`jpzip-go` は `jpzip.nadai.dev` から日本の郵便番号 120,677 件を引く Go SDK です。
登録不要、レート制限なし、API キー不要。

- 🇯🇵 **全件収録** — 漢字・カナ・ローマ字・自治体コード(JIS X 0401 / 総務省地方公共団体コード)
- ⚡️ **高速** — L1 LRU + 任意の L2 永続キャッシュ。`Preload` でネットワーク往復なしのルックアップが可能
- 🛡️ **堅牢** — 5xx / ネットワーク失敗時は指数バックオフで最大 3 回リトライ
- 🪶 **依存ゼロ** — 標準ライブラリのみ
- 🆓 **永久無料** — Cloudflare Pages 無料枠で運用(課金軸が存在しない)
- 🔌 **同一 API** — [全 jpzip SDK](#他言語版) で API が揃う

## 必要環境

Go 1.23+

## インストール

```bash
go get github.com/jpzip/go
```

## クイックスタート

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
        fmt.Println("見つかりません")
        return
    }
    fmt.Println(entry.Prefecture, entry.City, entry.Towns[0].Town)
    // 出力: 神奈川県 横浜市中区 港町
}
```

ローマ字・自治体コードも同じエントリに含まれます:

```go
fmt.Println(entry.PrefectureRoma, entry.CityRoma, entry.Towns[0].Roma)
// 出力: Kanagawa Ken Yokohama Shi Naka Ku Minatocho

fmt.Println(entry.PrefectureCode, entry.CityCode)
// 出力: 14 14104
```

## ユースケース

### 郵便番号ルックアップ HTTP エンドポイント (net/http)

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

### 郵便番号ルックアップ HTTP エンドポイント (chi)

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

### CSV のバッチ検証

```go
all, err := jpzip.LookupAll(ctx) // 全件をメモリに展開(JSON 約 37 MiB)
if err != nil {
    log.Fatal(err)
}
for _, zip := range csvZipcodes {
    if _, ok := all[zip]; !ok {
        log.Printf("不正な郵便番号: %s", zip)
    }
}
```

### キャッシュからの提供(任意の L2 バックエンド)

データは 948 個の 3 桁 prefix バケットに分割されています。デフォルト L1 (100 件) は
ホットなバケットを保持しますが、全件を常駐させるには L2 を併用するか
`WithMemoryCacheSize` を 948 超に設定してください。

```go
client := jpzip.New(
    jpzip.WithMemoryCacheSize(1024),
    jpzip.WithCache(myFileCache), // Cache インターフェース実装
)
if err := client.Preload(ctx, "all"); err != nil {
    log.Fatal(err)
}
// 以降の Lookup は L1/L2 で完結し、ネットワークにアクセスしない
entry, _ := client.Lookup(ctx, "2310017")
```

## API リファレンス

完全版は [pkg.go.dev](https://pkg.go.dev/github.com/jpzip/go) を参照。

### 関数(パッケージレベル、内部の default Client を共有)

| 関数 | 説明 |
|---|---|
| `Lookup(ctx, zipcode)` | 7 桁の郵便番号で 1 件引く。見つからない / 不正な入力は `(nil, nil)`(不正入力時はネットワーク不使用)。 |
| `LookupGroup(ctx, prefix)` | 1〜3 桁の prefix で引く。1 桁は `/g/{d}.json` を 1 回、3 桁は `/p/{ddd}.json` を 1 回、2 桁は 10 並列 fetch して結合。 |
| `LookupAll(ctx)` | `/g/0..9.json` を並列取得して全件(120k 件、約 37 MiB)を返す。 |
| `GetMeta(ctx)` | データバージョン・生成日時・都道府県別件数・spec version。`Refresh` までは結果をキャッシュ。 |
| `Preload(ctx, scope)` | `"all"` または特定 prefix で L1(L2 設定時は L2 も)を温める。 |
| `IsValidZipcode(s)` | 純粋な書式チェック(`^\d{7}$`)。ネットワーク不使用。 |

### クライアント(高度な用途)

`New()` で設定可能なインスタンスを取得。L2 キャッシュ、HTTP クライアント差し替え、配信元変更、複数の独立キャッシュが必要な場合に使用:

```go
client := jpzip.New(
    jpzip.WithBaseURL("https://jpzip.nadai.dev"),
    jpzip.WithHTTPClient(&http.Client{Timeout: 5 * time.Second}),
    jpzip.WithMemoryCacheSize(200), // L1 容量(prefix バケット数)、デフォルト 100
    jpzip.WithCache(myCache),       // L2(任意)
    jpzip.OnSpecMismatch(func(expected, received string) {
        log.Printf("jpzip spec 不一致: SDK=%s server=%s", expected, received)
    }),
)
```

Client は `Lookup` / `LookupGroup` / `LookupAll` / `GetMeta` / `Preload` に加えて:

| メソッド | 説明 |
|---|---|
| `client.Refresh(ctx)` | L1(L2 設定時は L2 も)を消し、キャッシュ済み meta を破棄。 |

`GetMeta` が `/meta.json` の `version` 変更を検知すると L1/L2 が自動クリアされます。データ切り替えに追従するには `GetMeta` を定期的に呼んでください。

### エラー

- `ErrInvalidPrefix` — prefix が 1〜3 桁でない場合に `LookupGroup` / `Preload` から(ラップして)返る。`errors.Is(err, jpzip.ErrInvalidPrefix)` で判定。
- ネットワーク失敗と 5xx は最大 3 回試行(初回 + リトライ 2 回)、指数バックオフのスリープは 400ms / 800ms。404 以外の 4xx は即座にエラー返却(404 は `(nil, nil)`)。

### `Cache` インターフェース

任意の L2 バックエンド(ファイル / BoltDB / Redis / KV など)を渡せます:

```go
type Cache interface {
    Get(ctx context.Context, key string) ([]byte, bool, error)
    Set(ctx context.Context, key string, value []byte) error
    Delete(ctx context.Context, key string) error
    Clear(ctx context.Context) error
}
```

キーは prefix バケットの完全 URL(例: `https://jpzip.nadai.dev/p/231.json`)、値は生 JSON バイト列。

## なぜ jpzip-go か

| | **jpzip-go** | [oirik/gokenall][gokenall] | [syumai/go-jpostcode][jpostcode] | [zipcloud API][zipcloud] |
|---|---|---|---|---|
| ローマ字(`Yokohama Shi`) | ✅ | ❌ | ❌ | ❌ |
| 自治体コード(JIS / 総務省) | ✅ | ⚠️ JIS のみ | ❌ | ❌ |
| CSV を手動 DL 不要 | ✅ | ❌ | ⚠️ 埋め込み | ✅ |
| 月次更新 | ✅ 自動 | ❌ 手動 | ❌ 手動 | ✅ |
| Preload 後オフライン | ✅ | ✅ | ✅ | ❌ |
| レート制限なし | ✅ | ✅ | ✅ | ⚠️ 大量アクセス非推奨 |
| L1 + 差し替え可能な L2 | ✅ | ❌ | ❌ | ❌ |
| 依存ゼロ | ✅ | ✅ | ✅ | n/a |

[gokenall]: https://github.com/oirik/gokenall
[jpostcode]: https://github.com/syumai/go-jpostcode
[zipcloud]: http://zipcloud.ibsnet.co.jp/doc/api

## 他言語版

全 SDK で同一の API を提供しています:

[TypeScript](https://github.com/jpzip/js) · [Python](https://github.com/jpzip/python) · [Rust](https://github.com/jpzip/rust) · [Ruby](https://github.com/jpzip/ruby) · [PHP](https://github.com/jpzip/php) · [Swift](https://github.com/jpzip/swift) · [Dart](https://github.com/jpzip/dart)

## 関連リソース

- **Web サイト** — https://jpzip.nadai.dev
- **プロトコル仕様** — [jpzip/spec](https://github.com/jpzip/spec)
- **データ ETL** — [jpzip/data](https://github.com/jpzip/data)
- **MCP サーバー** — [jpzip/mcp](https://github.com/jpzip/mcp) — Claude / ChatGPT / Cursor から jpzip を呼ぶ

## キーワード

日本郵便番号, 郵便番号, KEN_ALL, KEN_ALL_ROME, 住所検索, 住所自動補完, 住所バリデーション, japanese postal code, japan zipcode, golang japanese address, JIS X 0401, 総務省地方公共団体コード

## ライセンス

[MIT](./LICENSE)
