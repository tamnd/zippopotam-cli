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
// test server, so LookupAll (which builds its own full URL) hits the httptest
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

func TestLookupAll(t *testing.T) {
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
	places, err := c.LookupAll(context.Background(), "us", "90210")
	if err != nil {
		t.Fatal(err)
	}
	if len(places) != 1 {
		t.Fatalf("got %d places, want 1", len(places))
	}
	p := places[0]
	if p.PostCode != "90210" {
		t.Errorf("PostCode = %q, want 90210", p.PostCode)
	}
	if p.PlaceName != "Beverly Hills" {
		t.Errorf("PlaceName = %q, want Beverly Hills", p.PlaceName)
	}
	if p.Country != "United States" {
		t.Errorf("Country = %q, want United States", p.Country)
	}
	if p.CountryAbb != "US" {
		t.Errorf("CountryAbb = %q, want US", p.CountryAbb)
	}
	if p.State != "California" {
		t.Errorf("State = %q, want California", p.State)
	}
	if p.StateAbb != "CA" {
		t.Errorf("StateAbb = %q, want CA", p.StateAbb)
	}
	if p.Latitude != "34.0901" {
		t.Errorf("Latitude = %q, want 34.0901", p.Latitude)
	}
	if p.Longitude != "-118.4065" {
		t.Errorf("Longitude = %q, want -118.4065", p.Longitude)
	}
}

func TestLookupAllDefaultCountry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/us/90210" {
			t.Errorf("default country: unexpected path %q, want /us/90210", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleResponse))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	places, err := c.LookupAll(context.Background(), "", "90210")
	if err != nil {
		t.Fatal(err)
	}
	if len(places) == 0 {
		t.Fatal("got 0 places, want at least 1")
	}
	if places[0].Country != "United States" {
		t.Errorf("Country = %q, want United States", places[0].Country)
	}
}

func TestLookupAllNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	c.Retries = 0

	_, err := c.LookupAll(context.Background(), "us", "00000")
	if err == nil {
		t.Error("expected error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "no places found") {
		t.Errorf("error %q should contain 'no places found'", err.Error())
	}
}

func TestLookupAllInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	c.Retries = 0

	_, err := c.LookupAll(context.Background(), "us", "90210")
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestLookupAllMultiplePlaces(t *testing.T) {
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
	places, err := c.LookupAll(context.Background(), "us", "10001")
	if err != nil {
		t.Fatal(err)
	}
	// both places emitted
	if len(places) != 2 {
		t.Fatalf("got %d places, want 2", len(places))
	}
	if places[0].PlaceName != "New York City" {
		t.Errorf("places[0].PlaceName = %q, want New York City", places[0].PlaceName)
	}
	if places[1].PlaceName != "Manhattan" {
		t.Errorf("places[1].PlaceName = %q, want Manhattan", places[1].PlaceName)
	}
	// both records carry the same PostCode
	for i, p := range places {
		if p.PostCode != "10001" {
			t.Errorf("places[%d].PostCode = %q, want 10001", i, p.PostCode)
		}
	}
}
