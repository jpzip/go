// Package jpzip is the Go SDK for the jpzip postal-code dataset
// (https://jpzip.nadai.dev). The SDK fetches normalized JSON from the CDN,
// keeps a per-prefix in-memory LRU, and optionally backs that with a
// user-supplied persistent cache.
package jpzip

// SpecVersion is the jpzip protocol version this SDK targets.
const SpecVersion = "1.0"

// DefaultBaseURL is the production CDN origin.
const DefaultBaseURL = "https://jpzip.nadai.dev"

// Town corresponds to one element of ZipcodeEntry.Towns.
type Town struct {
	Town string `json:"town"`
	Kana string `json:"kana"`
	Roma string `json:"roma"`
	Note string `json:"note,omitempty"`
}

// ZipcodeEntry is one logical entry as published by the CDN.
type ZipcodeEntry struct {
	Prefecture     string `json:"prefecture"`
	PrefectureKana string `json:"prefecture_kana"`
	PrefectureRoma string `json:"prefecture_roma"`
	PrefectureCode string `json:"prefecture_code"`
	City           string `json:"city"`
	CityKana       string `json:"city_kana"`
	CityRoma       string `json:"city_roma"`
	CityCode       string `json:"city_code"`
	Towns          []Town `json:"towns"`
}

// ZipcodeDict is the on-the-wire shape of /all.json, /g/*.json, /p/*.json.
type ZipcodeDict = map[string]ZipcodeEntry

// Endpoints is part of /meta.json.
type Endpoints struct {
	All    string `json:"all"`
	Group  string `json:"group"`
	Prefix string `json:"prefix"`
}

// Meta is /meta.json.
type Meta struct {
	Version       string         `json:"version"`
	GeneratedAt   string         `json:"generated_at"`
	SpecVersion   string         `json:"spec_version"`
	TotalZipcodes int            `json:"total_zipcodes"`
	PrefixCount   int            `json:"prefix_count"`
	ByPref        map[string]int `json:"by_pref"`
	DataSource    string         `json:"data_source"`
	Endpoints     Endpoints      `json:"endpoints"`
}
