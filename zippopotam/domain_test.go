package zippopotam

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// These tests are offline: they exercise the URI driver's pure string functions
// and the host wiring, which need no network. The client's HTTP behaviour is
// covered in zippopotam_test.go.
//
// The URI authority for ZipCode records is "zipcode" (kit derives it from
// strings.ToLower("ZipCode")). The id is the bare postal code from PostCode.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "zippopotam" {
		t.Errorf("Scheme = %q, want zippopotam", info.Scheme)
	}
	found := false
	for _, h := range info.Hosts {
		if h == Host {
			found = true
		}
	}
	if !found {
		t.Errorf("Hosts = %v, want to contain %s", info.Hosts, Host)
	}
	if info.Identity.Binary != "zippopotam" {
		t.Errorf("Identity.Binary = %q, want zippopotam", info.Identity.Binary)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		in  string
		typ string
		id  string
	}{
		// bare postal code → default us
		{"90210", "zipcode", "90210"},
		// country-prefix form → strip prefix, bare code
		{"us:90210", "zipcode", "90210"},
		{"gb:ec1a", "zipcode", "ec1a"},
		{"de:10115", "zipcode", "10115"},
		// full API URL → extract postal code from path
		{"https://" + Host + "/us/90210", "zipcode", "90210"},
		{"https://" + WebHost + "/gb/ec1a", "zipcode", "ec1a"},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if err != nil || typ != tc.typ || id != tc.id {
			t.Errorf("Classify(%q) = (%q, %q, %v), want (%q, %q, nil)",
				tc.in, typ, id, err, tc.typ, tc.id)
		}
	}
}

func TestClassifyEmpty(t *testing.T) {
	_, _, err := Domain{}.Classify("")
	if err == nil {
		t.Error("Classify(\"\") expected error, got nil")
	}
}

func TestLocate(t *testing.T) {
	got, err := Domain{}.Locate("zipcode", "90210")
	want := "https://" + WebHost + "/90210"
	if err != nil || got != want {
		t.Errorf("Locate = (%q, %v), want (%q, nil)", got, err, want)
	}
}

func TestLocateUnknownType(t *testing.T) {
	_, err := Domain{}.Locate("page", "foo")
	if err == nil {
		t.Error("Locate with unknown type expected error, got nil")
	}
}

// TestHostWiring mounts the driver in a kit Host and checks the round trip:
// a ZipCode record mints to its URI (zippopotam://zipcode/<postcode>), and a
// bare postal code resolves to the same URI scheme.
func TestHostWiring(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}

	z := &ZipCode{
		PostCode:    "90210",
		Country:     "United States",
		CountryCode: "US",
		PlaceName:   "Beverly Hills",
		State:       "California",
		StateCode:   "CA",
		Lat:         "34.0901",
		Lon:         "-118.4065",
	}
	u, err := h.Mint(z)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if want := "zippopotam://zipcode/90210"; u.String() != want {
		t.Errorf("Mint = %q, want %q", u.String(), want)
	}

	got, err := h.ResolveOn("zippopotam", "90210")
	if err != nil {
		t.Fatalf("ResolveOn: %v", err)
	}
	if want := "zippopotam://zipcode/90210"; got.String() != want {
		t.Errorf("ResolveOn = %q, want %q", got.String(), want)
	}
}
