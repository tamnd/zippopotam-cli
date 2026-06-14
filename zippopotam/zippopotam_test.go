package zippopotam_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/zippopotam-cli/zippopotam"
)

// sampleResponse mimics the real api.zippopotam.us JSON shape (field names with spaces).
const sampleResponse = `{
	"post code": "90210",
	"country": "United States",
	"country abbreviation": "US",
	"places": [{
		"place name": "Beverly Hills",
		"longitude": "-118.4065",
		"state": "California",
		"state abbreviation": "CA",
		"latitude": "34.0901"
	}]
}`

// prefixTransport rewrites the host+scheme of every outgoing request to the
// test server, so Lookup (which builds its own full URL) hits the httptest
// server instead of the real API.
type prefixTransport struct {
	scheme string
	host   string
}

func newPrefixTransport(baseURL string) *prefixTransport {
	scheme, host, _ := strings.Cut(baseURL, "://")
	return &prefixTransport{scheme: scheme, host: host}
}

func (t *prefixTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = t.scheme
	clone.URL.Host = t.host
	return http.DefaultTransport.RoundTrip(clone)
}

func newTestClient(srv *httptest.Server) *zippopotam.Client {
	c := zippopotam.NewClient()
	c.Rate = 0
	c.HTTP = &http.Client{
		Timeout:   5 * time.Second,
		Transport: newPrefixTransport(srv.URL),
	}
	return c
}

func TestNewClient(t *testing.T) {
	c := zippopotam.NewClient()
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	if c.UserAgent == "" {
		t.Error("UserAgent is empty")
	}
}

func TestGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := zippopotam.NewClient()
	c.Rate = 0

	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	c := zippopotam.NewClient()
	c.Rate = 0
	c.Retries = 5

	start := time.Now()
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "recovered" {
		t.Errorf("body = %q after retries", body)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

func TestLookup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/us/90210" {
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleResponse))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	z, err := c.Lookup(context.Background(), "us", "90210")
	if err != nil {
		t.Fatal(err)
	}
	if z.PostCode != "90210" {
		t.Errorf("PostCode = %q, want 90210", z.PostCode)
	}
	if z.PlaceName != "Beverly Hills" {
		t.Errorf("PlaceName = %q, want Beverly Hills", z.PlaceName)
	}
	if z.Country != "United States" {
		t.Errorf("Country = %q, want United States", z.Country)
	}
	if z.CountryCode != "US" {
		t.Errorf("CountryCode = %q, want US", z.CountryCode)
	}
	if z.State != "California" {
		t.Errorf("State = %q, want California", z.State)
	}
	if z.StateCode != "CA" {
		t.Errorf("StateCode = %q, want CA", z.StateCode)
	}
	if z.Lat != "34.0901" {
		t.Errorf("Lat = %q, want 34.0901", z.Lat)
	}
	if z.Lon != "-118.4065" {
		t.Errorf("Lon = %q, want -118.4065", z.Lon)
	}
}

func TestLookupDefaultCountry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/us/90210" {
			t.Errorf("default country: unexpected path %q, want /us/90210", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleResponse))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	z, err := c.Lookup(context.Background(), "", "90210")
	if err != nil {
		t.Fatal(err)
	}
	if z.CountryCode != "US" {
		t.Errorf("CountryCode = %q, want US", z.CountryCode)
	}
}

func TestLookupNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	c.Retries = 0

	_, err := c.Lookup(context.Background(), "us", "00000")
	if err == nil {
		t.Error("expected error for 404, got nil")
	}
}

func TestLookupInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	c.Retries = 0

	_, err := c.Lookup(context.Background(), "us", "90210")
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestLookupMultiplePlaces(t *testing.T) {
	resp := map[string]interface{}{
		"post code":            "10001",
		"country":              "United States",
		"country abbreviation": "US",
		"places": []map[string]interface{}{
			{
				"place name":         "New York City",
				"longitude":          "-73.9967",
				"state":              "New York",
				"state abbreviation": "NY",
				"latitude":           "40.7484",
			},
			{
				"place name":         "Manhattan",
				"longitude":          "-73.9967",
				"state":              "New York",
				"state abbreviation": "NY",
				"latitude":           "40.7484",
			},
		},
	}
	b, _ := json.Marshal(resp)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	z, err := c.Lookup(context.Background(), "us", "10001")
	if err != nil {
		t.Fatal(err)
	}
	// should return first place
	if z.PlaceName != "New York City" {
		t.Errorf("PlaceName = %q, want New York City (first place)", z.PlaceName)
	}
}
