package jpzip

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	zipRegex    = regexp.MustCompile(`^\d{7}$`)
	prefixRegex = regexp.MustCompile(`^\d{1,3}$`)
)

// ErrInvalidPrefix is returned for prefixes that aren't 1-3 digits.
var ErrInvalidPrefix = errors.New("jpzip: prefix must be 1-3 digits")

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the CDN origin.
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }

// WithHTTPClient swaps the underlying *http.Client (useful for tests).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithMemoryCacheSize sets the L1 capacity in prefix entries. Default 100.
func WithMemoryCacheSize(n int) Option { return func(c *Client) { c.memCap = n } }

// WithCache enables an L2 persistent cache.
func WithCache(cache Cache) Option { return func(c *Client) { c.cache = cache } }

// OnSpecMismatch sets a hook invoked once if /meta.json's spec_version
// differs from this SDK's SpecVersion.
func OnSpecMismatch(fn func(expected, received string)) Option {
	return func(c *Client) { c.onSpecMismatch = fn }
}

// Client is the jpzip SDK entry point.
type Client struct {
	baseURL        string
	http           *http.Client
	cache          Cache
	memCap         int
	onSpecMismatch func(expected, received string)

	memOnce sync.Once
	mem     *memoryLRU

	metaMu       sync.Mutex
	metaCached   *Meta
	metaResolved bool
	knownVersion string
}

// New constructs a Client. Without options it uses the default base URL,
// the default http.Client, an L1 cache of 100, and no L2 cache.
func New(opts ...Option) *Client {
	c := &Client{
		baseURL: DefaultBaseURL,
		http:    &http.Client{Timeout: 30 * time.Second},
		memCap:  100,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) memCache() *memoryLRU {
	c.memOnce.Do(func() { c.mem = newMemoryLRU(c.memCap) })
	return c.mem
}

// Lookup returns the entry for zipcode, or (nil, nil) if not found.
// Malformed input returns (nil, nil) without contacting the network.
func (c *Client) Lookup(ctx context.Context, zipcode string) (*ZipcodeEntry, error) {
	if !zipRegex.MatchString(zipcode) {
		return nil, nil
	}
	dict, err := c.fetchPrefixDict(ctx, zipcode[:3])
	if err != nil {
		return nil, err
	}
	if dict == nil {
		return nil, nil
	}
	if e, ok := dict[zipcode]; ok {
		return &e, nil
	}
	return nil, nil
}

// LookupGroup fetches all entries under a 1-, 2-, or 3-digit prefix.
// A 2-digit prefix fans out into 10 prefix-3 fetches.
func (c *Client) LookupGroup(ctx context.Context, prefix string) (ZipcodeDict, error) {
	if !prefixRegex.MatchString(prefix) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidPrefix, prefix)
	}
	switch len(prefix) {
	case 3:
		d, err := c.fetchPrefixDict(ctx, prefix)
		if err != nil {
			return nil, err
		}
		if d == nil {
			return ZipcodeDict{}, nil
		}
		return d, nil
	case 1:
		return c.fetchURL(ctx, c.baseURL+"/g/"+prefix+".json")
	case 2:
		// Fan out in parallel.
		var wg sync.WaitGroup
		results := make([]ZipcodeDict, 10)
		errs := make([]error, 10)
		for i := 0; i < 10; i++ {
			i := i
			wg.Add(1)
			go func() {
				defer wg.Done()
				d, err := c.fetchPrefixDict(ctx, fmt.Sprintf("%s%d", prefix, i))
				results[i] = d
				errs[i] = err
			}()
		}
		wg.Wait()
		out := make(ZipcodeDict)
		for i, d := range results {
			if errs[i] != nil {
				return nil, errs[i]
			}
			for k, v := range d {
				out[k] = v
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("%w: %q", ErrInvalidPrefix, prefix)
}

// LookupAll fetches /all.json.
func (c *Client) LookupAll(ctx context.Context) (ZipcodeDict, error) {
	return c.fetchURL(ctx, c.baseURL+"/all.json")
}

// GetMeta returns the cached /meta.json. The first call hits the network;
// subsequent calls reuse the result until Refresh() is called.
func (c *Client) GetMeta(ctx context.Context) (*Meta, error) {
	c.metaMu.Lock()
	if c.metaResolved {
		m := c.metaCached
		c.metaMu.Unlock()
		return m, nil
	}
	c.metaMu.Unlock()

	body, status, err := c.getRaw(ctx, c.baseURL+"/meta.json")
	if err != nil {
		return nil, err
	}
	c.metaMu.Lock()
	defer c.metaMu.Unlock()
	if status == http.StatusNotFound {
		c.metaResolved = true
		return nil, nil
	}
	var m Meta
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("jpzip: parse meta: %w", err)
	}
	if m.SpecVersion != SpecVersion && c.onSpecMismatch != nil {
		c.onSpecMismatch(SpecVersion, m.SpecVersion)
	}
	if c.knownVersion != "" && c.knownVersion != m.Version {
		c.memCache().clear()
		if c.cache != nil {
			_ = c.cache.Clear(ctx)
		}
	}
	c.knownVersion = m.Version
	c.metaCached = &m
	c.metaResolved = true
	return &m, nil
}

// Preload pulls the requested scope into L1 (and L2 when configured).
// `scope == "all"` downloads /all.json; otherwise it must be a valid prefix.
func (c *Client) Preload(ctx context.Context, scope string) error {
	if scope == "all" {
		dict, err := c.LookupAll(ctx)
		if err != nil {
			return err
		}
		// Bucket by 3-digit prefix and seed L1 + L2.
		buckets := make(map[string]ZipcodeDict)
		for zip, e := range dict {
			p := zip[:3]
			if buckets[p] == nil {
				buckets[p] = make(ZipcodeDict)
			}
			buckets[p][zip] = e
		}
		for p, b := range buckets {
			url := c.prefixURL(p)
			c.memCache().set(url, b)
			if err := c.writeL2(ctx, url, b); err != nil {
				return err
			}
		}
		return nil
	}
	if !prefixRegex.MatchString(scope) {
		return fmt.Errorf("%w: %q", ErrInvalidPrefix, scope)
	}
	_, err := c.LookupGroup(ctx, scope)
	return err
}

// Refresh wipes L1 (and L2 when configured) and forgets the cached meta.
func (c *Client) Refresh(ctx context.Context) error {
	c.memCache().clear()
	c.metaMu.Lock()
	c.metaCached = nil
	c.metaResolved = false
	c.knownVersion = ""
	c.metaMu.Unlock()
	if c.cache != nil {
		return c.cache.Clear(ctx)
	}
	return nil
}

/* ------------------------------ internals ------------------------------- */

func (c *Client) prefixURL(prefix3 string) string {
	return c.baseURL + "/p/" + prefix3 + ".json"
}

// fetchPrefixDict resolves /p/{prefix3}.json through L1 → L2 → network.
func (c *Client) fetchPrefixDict(ctx context.Context, prefix3 string) (ZipcodeDict, error) {
	url := c.prefixURL(prefix3)
	if d, ok := c.memCache().get(url); ok {
		return d, nil
	}
	if d, err := c.readL2(ctx, url); err != nil {
		return nil, err
	} else if d != nil {
		c.memCache().set(url, d)
		return d, nil
	}
	d, err := c.fetchURL(ctx, url)
	if err != nil {
		return nil, err
	}
	if d != nil {
		c.memCache().set(url, d)
		if err := c.writeL2(ctx, url, d); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func (c *Client) fetchURL(ctx context.Context, url string) (ZipcodeDict, error) {
	body, status, err := c.getRaw(ctx, url)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, nil
	}
	var d ZipcodeDict
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, fmt.Errorf("jpzip: parse %s: %w", url, err)
	}
	return d, nil
}

func (c *Client) readL2(ctx context.Context, url string) (ZipcodeDict, error) {
	if c.cache == nil {
		return nil, nil
	}
	bytes, ok, err := c.cache.Get(ctx, url)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	var d ZipcodeDict
	if err := json.Unmarshal(bytes, &d); err != nil {
		// corrupt cache — drop it
		_ = c.cache.Delete(ctx, url)
		return nil, nil
	}
	return d, nil
}

func (c *Client) writeL2(ctx context.Context, url string, d ZipcodeDict) error {
	if c.cache == nil {
		return nil
	}
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return c.cache.Set(ctx, url, b)
}

// getRaw GETs url with bounded retries on 5xx / network failures and
// returns (body, statusCode, nil) on success. 404 is returned with a nil
// body so callers can distinguish "absent" from "fetch error".
func (c *Client) getRaw(ctx context.Context, url string) ([]byte, int, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			d := time.Duration(200<<attempt) * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(d):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("Accept", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode == http.StatusNotFound {
			return nil, http.StatusNotFound, nil
		}
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("jpzip: %s returned %d", url, resp.StatusCode)
			continue
		}
		if resp.StatusCode >= 400 {
			return nil, resp.StatusCode, fmt.Errorf("jpzip: %s returned %d", url, resp.StatusCode)
		}
		return body, resp.StatusCode, nil
	}
	return nil, 0, lastErr
}

// IsValidZipcode reports whether s syntactically looks like a 7-digit
// zipcode (no fetch involved).
func IsValidZipcode(s string) bool { return zipRegex.MatchString(s) }
