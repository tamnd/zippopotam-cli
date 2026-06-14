// Package zippopotam is the library behind the zippopotam command line:
// the HTTP client, request shaping, and the typed data models for zippopotam.
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests so a busy session stays polite, and retries the
// transient failures (429 and 5xx) that any public site throws under load.
package zippopotam

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultUserAgent identifies the client to the API. A real, honest
// User-Agent is both polite and the thing most likely to keep you unblocked.
const DefaultUserAgent = "zippopotam/dev (+https://github.com/tamnd/zippopotam-cli)"

// Host is the API host this client talks to.
const Host = "api.zippopotam.us"

// WebHost is the site homepage used in Locate.
const WebHost = "www.zippopotam.us"

// BaseURL is the root every request is built from.
const BaseURL = "https://" + Host

// Client talks to the Zippopotam API over HTTP.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	// Rate is the minimum gap between requests. Zero means no pacing.
	Rate    time.Duration
	Retries int

	last time.Time
}

// NewClient returns a Client with sensible defaults: a 30s timeout, a 200ms
// minimum gap between requests, and five retries on transient errors.
func NewClient() *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		UserAgent: DefaultUserAgent,
		Rate:      200 * time.Millisecond,
		Retries:   5,
	}
}

// Place is the output record for a postal code lookup. One record is emitted
// per place returned by the API; a single postal code can match many places.
type Place struct {
	PostCode  string `kit:"id" json:"postcode"`
	PlaceName string `json:"place_name"`
	State     string `json:"state"`
	Country   string `json:"country"`
	Latitude  string `json:"latitude"`
	Longitude string `json:"longitude"`
}

// wire types for decoding the API response (field names have spaces).

type wireZip struct {
	PostCode    string      `json:"post code"`
	Country     string      `json:"country"`
	CountryAbbr string      `json:"country abbreviation"`
	Places      []wirePlace `json:"places"`
}

type wirePlace struct {
	PlaceName string `json:"place name"`
	Longitude string `json:"longitude"`
	State     string `json:"state"`
	StateAbbr string `json:"state abbreviation"`
	Latitude  string `json:"latitude"`
}

// LookupAll fetches a postal code from the given country and returns one Place
// record per matching place. country defaults to "us" when empty. A 404 from
// the API is returned as an explicit "no places found" error.
func (c *Client) LookupAll(ctx context.Context, country, postalCode string) ([]*Place, error) {
	if country == "" {
		country = "us"
	}
	country = strings.ToLower(strings.TrimSpace(country))
	postalCode = strings.TrimSpace(postalCode)

	url := BaseURL + "/" + country + "/" + postalCode
	body, err := c.Get(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("no places found for %s/%s", country, postalCode)
	}

	var w wireZip
	if err := json.Unmarshal(body, &w); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if len(w.Places) == 0 {
		return nil, fmt.Errorf("no places found for %s/%s", country, postalCode)
	}

	places := make([]*Place, 0, len(w.Places))
	for _, p := range w.Places {
		places = append(places, &Place{
			PostCode:  w.PostCode,
			PlaceName: p.PlaceName,
			State:     p.State,
			Country:   w.Country,
			Latitude:  p.Latitude,
			Longitude: p.Longitude,
		})
	}
	return places, nil
}

// Get fetches url and returns the response body. It paces and retries according
// to the client's settings. The caller owns nothing extra; the body is read
// fully and closed here.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, url)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", url, lastErr)
}

func (c *Client) do(ctx context.Context, url string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	if c.Rate <= 0 {
		return
	}
	if wait := c.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}
