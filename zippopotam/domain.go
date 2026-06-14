package zippopotam

import (
	"context"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes zippopotam as a kit Domain: a driver that a multi-domain
// host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/zippopotam-cli/zippopotam"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then dereferences
// zippopotam:// URIs by routing to the operations Register installs. The same
// Domain also builds the standalone zippopotam binary (see cli.NewApp), so the
// binary and a host share one source of truth.
func init() { kit.Register(Domain{}) }

// Domain is the zippopotam driver. It carries no state; the per-run client is
// built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against, and
// the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "zippopotam",
		Hosts:  []string{Host, WebHost},
		Identity: kit.Identity{
			Binary: "zippopotam",
			Short:  "A command line for Zippopotam.us postal code lookups.",
			Long: `A command line for Zippopotam.us postal code lookups.

zippopotam reads public postal code data over plain HTTPS, shapes it into
clean records, and prints output that pipes into the rest of your tools. No API
key, nothing to run alongside it.`,
			Site: "https://" + WebHost,
			Repo: "https://github.com/tamnd/zippopotam-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
// The op emits *ZipCode, so kit derives the URI authority as "zipcode"
// (strings.ToLower("ZipCode")).
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{
		Name:    "lookup",
		Group:   "read",
		Single:  true,
		Summary: "Look up a postal code",
		Args:    []kit.Arg{{Name: "postalcode", Help: "postal/zip code"}},
	}, lookupOp)
}

// newClient builds the client from the host-resolved config, so a host and the
// standalone binary pace and identify themselves the same way.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClient()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.HTTP.Timeout = cfg.Timeout
	}
	return c, nil
}

// --- inputs ---

type lookupInput struct {
	PostalCode string  `kit:"arg" help:"postal/zip code"`
	Country    string  `kit:"flag" help:"country code (us, gb, de, fr, etc.)" default:"us"`
	Client     *Client `kit:"inject"`
}

// --- handlers ---

func lookupOp(ctx context.Context, in lookupInput, emit func(*ZipCode) error) error {
	z, err := in.Client.Lookup(ctx, in.Country, in.PostalCode)
	if err != nil {
		return mapErr(err)
	}
	return emit(z)
}

// --- Resolver: the URI-native string functions, pure and network-free ---

// Classify turns an accepted input — a bare postal code, an optional
// "CC:postalcode" prefix form, or a full zippopotam URL — into (uriType, id).
// The URI authority for ZipCode records is "zipcode" (kit derives it from the
// struct name). The id is the bare postal code stored in ZipCode.PostCode.
func (Domain) Classify(input string) (uriType, id string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", errs.Usage("empty input")
	}

	// Full URL: https://api.zippopotam.us/us/90210 or https://www.zippopotam.us/us/90210
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		for _, h := range []string{Host, WebHost} {
			for _, scheme := range []string{"https://", "http://"} {
				prefix := scheme + h + "/"
				if strings.HasPrefix(input, prefix) {
					path := strings.Trim(strings.TrimPrefix(input, prefix), "/")
					// path is "countrycode/postalcode"; extract the postal code
					if path != "" {
						parts := strings.SplitN(path, "/", 2)
						if len(parts) == 2 && parts[1] != "" {
							return "zipcode", parts[1], nil
						}
						// bare path with no slash — treat as postal code
						return "zipcode", path, nil
					}
				}
			}
		}
		return "", "", errs.Usage("unrecognized zippopotam URL: %q", input)
	}

	// "XX:postalcode" country-prefix form — strip the country prefix, return bare code
	if idx := strings.Index(input, ":"); idx == 2 {
		code := strings.TrimSpace(input[idx+1:])
		if code != "" {
			return "zipcode", code, nil
		}
	}

	// bare postalcode
	return "zipcode", input, nil
}

// Locate is the inverse: the live https URL for a (uriType, id).
// id is the bare postal code. We link to the www homepage since the API
// doesn't have a human-readable page per code.
func (Domain) Locate(uriType, id string) (string, error) {
	if uriType != "zipcode" {
		return "", errs.Usage("zippopotam has no resource type %q", uriType)
	}
	return "https://" + WebHost + "/" + strings.Trim(id, "/"), nil
}

// mapErr converts a library error into the kit error kind that carries the right
// exit code, so a host renders the same outcomes the standalone binary does.
func mapErr(err error) error {
	return err
}
