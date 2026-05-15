package jpzip

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func baseEntry() ZipcodeEntry {
	return ZipcodeEntry{
		Prefecture:     "神奈川県",
		PrefectureKana: "カナガワケン",
		PrefectureRoma: "Kanagawa",
		PrefectureCode: "14",
		City:           "横浜市中区",
		CityKana:       "ヨコハマシナカク",
		CityRoma:       "Yokohama Shi Naka Ku",
		CityCode:       "14104",
		Towns:          []Town{{Town: "矢口台", Kana: "ヤグチダイ", Roma: "Yaguchidai"}},
	}
}

func newServer(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return New(WithBaseURL(srv.URL)), srv, &hits
}

func TestLookupMalformedNoFetch(t *testing.T) {
	client, _, hits := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	got, err := client.Lookup(context.Background(), "abc")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
	if atomic.LoadInt32(hits) != 0 {
		t.Fatalf("expected 0 hits, got %d", atomic.LoadInt32(hits))
	}
}

func TestLookupHit(t *testing.T) {
	dict := ZipcodeDict{"2310831": baseEntry()}
	client, _, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/p/231.json" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(dict)
	})
	got, err := client.Lookup(context.Background(), "2310831")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got == nil || got.Prefecture != "神奈川県" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestLookupL1Caching(t *testing.T) {
	dict := ZipcodeDict{"2310831": baseEntry()}
	client, _, hits := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(dict)
	})
	for i := 0; i < 5; i++ {
		if _, err := client.Lookup(context.Background(), "2310831"); err != nil {
			t.Fatal(err)
		}
	}
	if atomic.LoadInt32(hits) != 1 {
		t.Fatalf("expected 1 fetch, got %d", atomic.LoadInt32(hits))
	}
}

func TestLookupGroupTwoDigitFanout(t *testing.T) {
	dict := ZipcodeDict{"2310831": baseEntry()}
	var fetchedPaths sync.Map
	client, _, hits := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		fetchedPaths.Store(r.URL.Path, true)
		if r.URL.Path == "/p/231.json" {
			_ = json.NewEncoder(w).Encode(dict)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	got, err := client.LookupGroup(context.Background(), "23")
	if err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(hits) != 10 {
		t.Fatalf("expected 10 fetches, got %d", atomic.LoadInt32(hits))
	}
	if _, ok := got["2310831"]; !ok {
		t.Fatalf("missing 2310831 in %+v", got)
	}
}

func TestLookupGroupInvalid(t *testing.T) {
	client, _, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	_, err := client.LookupGroup(context.Background(), "abcd")
	if !errors.Is(err, ErrInvalidPrefix) {
		t.Fatalf("expected ErrInvalidPrefix, got %v", err)
	}
}

func TestPreloadAllSeedsL1(t *testing.T) {
	dict := ZipcodeDict{"2310831": baseEntry()}
	client, _, hits := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/all.json" {
			_ = json.NewEncoder(w).Encode(dict)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	if err := client.Preload(context.Background(), "all"); err != nil {
		t.Fatal(err)
	}
	got, err := client.Lookup(context.Background(), "2310831")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.City != "横浜市中区" {
		t.Fatalf("unexpected: %+v", got)
	}
	if atomic.LoadInt32(hits) != 1 {
		t.Fatalf("expected 1 fetch, got %d", atomic.LoadInt32(hits))
	}
}

func TestGetMetaCachedAndMismatch(t *testing.T) {
	meta := Meta{
		Version: "2026-05", GeneratedAt: "2026-05-01T00:00:00Z",
		SpecVersion: "2.0", // mismatch
		TotalZipcodes: 1, PrefixCount: 1,
		ByPref:     map[string]int{"14": 1},
		DataSource: "https://example.com",
		Endpoints:  Endpoints{All: "/all.json", Group: "/g/{prefix1}.json", Prefix: "/p/{prefix3}.json"},
	}
	var mismatches int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(meta)
	}))
	t.Cleanup(srv.Close)

	c := New(WithBaseURL(srv.URL), OnSpecMismatch(func(_, _ string) {
		atomic.AddInt32(&mismatches, 1)
	}))
	m1, err := c.GetMeta(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m2, err := c.GetMeta(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if m1 == nil || m2 == nil || m1 != m2 {
		t.Fatalf("expected same cached pointer, got %p / %p", m1, m2)
	}
	if got := atomic.LoadInt32(&mismatches); got != 1 {
		t.Fatalf("expected 1 mismatch call, got %d", got)
	}
}

func TestL2Cache(t *testing.T) {
	cache := newMemMapCache()
	dict := ZipcodeDict{"2310831": baseEntry()}
	var fetches int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fetches, 1)
		_ = json.NewEncoder(w).Encode(dict)
	}))
	t.Cleanup(srv.Close)

	c1 := New(WithBaseURL(srv.URL), WithCache(cache))
	if _, err := c1.Lookup(context.Background(), "2310831"); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&fetches) != 1 {
		t.Fatalf("c1 first call: expected 1 fetch, got %d", atomic.LoadInt32(&fetches))
	}

	// New client, fresh L1 — should hit L2 instead of the network.
	c2 := New(WithBaseURL(srv.URL), WithCache(cache))
	got, err := c2.Lookup(context.Background(), "2310831")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatalf("expected entry from L2")
	}
	if atomic.LoadInt32(&fetches) != 1 {
		t.Fatalf("c2 should have used L2; got fetches = %d", atomic.LoadInt32(&fetches))
	}
}

func TestMemoryLRUEviction(t *testing.T) {
	c := newMemoryLRU(2)
	c.set("a", ZipcodeDict{})
	c.set("b", ZipcodeDict{})
	c.set("c", ZipcodeDict{})
	if _, ok := c.get("a"); ok {
		t.Fatalf("a should have been evicted")
	}
	if _, ok := c.get("b"); !ok {
		t.Fatalf("b should be cached")
	}
	if _, ok := c.get("c"); !ok {
		t.Fatalf("c should be cached")
	}
}
