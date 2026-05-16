# jpzip — Go SDK

> 日本の郵便番号を CDN 配信の JSON データから引く Go SDK。

- 配信ドメイン: `https://jpzip.nadai.dev`
- プロトコル仕様: [`jpzip/spec`](https://github.com/jpzip/spec)
- データ ETL: [`jpzip/data`](https://github.com/jpzip/data)

```sh
go get github.com/jpzip/go
```

## 使い方

### 関数 API

```go
import (
  "context"

  jpzip "github.com/jpzip/go"
)

ctx := context.Background()
entry, err := jpzip.Lookup(ctx, "2310017")
// entry == nil なら見つからなかった

dict, err := jpzip.LookupGroup(ctx, "23")  // 2 桁は 10 並列 fetch
all, err  := jpzip.LookupAll(ctx)
meta, err := jpzip.GetMeta(ctx)
```

### クライアント API (L2 キャッシュ・複数インスタンス用)

```go
client := jpzip.New(
    jpzip.WithBaseURL("https://jpzip.nadai.dev"),
    jpzip.WithMemoryCacheSize(200),
    jpzip.WithCache(myCache),  // Cache インターフェースを実装
)

if err := client.Preload(ctx, "all"); err != nil {
    log.Fatal(err)
}
entry, err := client.Lookup(ctx, "2310017")
```

## Cache インターフェース

```go
type Cache interface {
    Get(ctx context.Context, key string) ([]byte, bool, error)
    Set(ctx context.Context, key string, value []byte) error
    Delete(ctx context.Context, key string) error
    Clear(ctx context.Context) error
}
```

ファイル / KV / Redis 等の任意の実装を渡せる。

## 入力検証

`Lookup()` は `^\d{7}$` にマッチしない入力には fetch せず `(nil, nil)` を返す。

## バージョン整合性

`GetMeta()` で spec_version が異なる場合、`OnSpecMismatch` で渡したコールバックが 1 度だけ呼ばれる。データバージョンが変わったら L1/L2 を自動 invalidate する。

## ライセンス

[MIT](./LICENSE)
