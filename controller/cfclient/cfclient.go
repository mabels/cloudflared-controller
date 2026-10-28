package cfclient

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/dns"
	"github.com/cloudflare/cloudflare-go/v7/option"
	"github.com/cloudflare/cloudflare-go/v7/shared"
	"github.com/cloudflare/cloudflare-go/v7/zero_trust"
	"github.com/cloudflare/cloudflare-go/v7/zones"
	"github.com/rs/zerolog"
)

const (
	zoneCacheTTL = 10 * time.Minute
	// a hostname without a known zone triggers a refresh at most this often
	zoneMissRefresh = time.Minute
	routeCacheTTL   = time.Hour
	dnsComment      = "managed by cloudflared-controller"
)

type routed struct {
	tunnelID string
	at       time.Time
}

type Client struct {
	api       *cloudflare.Client
	accountID string
	log       *zerolog.Logger
	now       func() time.Time

	zonesLock    sync.Mutex
	zones        []zones.Zone
	zonesFetched time.Time

	routedLock sync.Mutex
	routed     map[string]routed
}

func New(apiURL, apiToken, accountID string, log *zerolog.Logger) *Client {
	opts := []option.RequestOption{option.WithAPIToken(apiToken)}
	if apiURL != "" {
		if !strings.HasSuffix(apiURL, "/") {
			apiURL += "/"
		}
		opts = append(opts, option.WithBaseURL(apiURL))
	}
	return &Client{
		api:       cloudflare.NewClient(opts...),
		accountID: accountID,
		log:       log,
		now:       time.Now,
		routed:    make(map[string]routed),
	}
}

func (c *Client) API() *cloudflare.Client {
	return c.api
}

func (c *Client) AccountID() string {
	return c.accountID
}

func (c *Client) FindTunnel(ctx context.Context, name string) (*shared.CloudflareTunnel, error) {
	iter := c.api.ZeroTrust.Tunnels.Cloudflared.ListAutoPaging(ctx, zero_trust.TunnelCloudflaredListParams{
		AccountID: cloudflare.F(c.accountID),
		Name:      cloudflare.F(name),
		IsDeleted: cloudflare.F(false),
	})
	for iter.Next() {
		t := iter.Current()
		if t.Name == name && t.DeletedAt.IsZero() {
			return &t, nil
		}
	}
	return nil, iter.Err()
}

// GetTunnel returns nil, nil only if Cloudflare says the tunnel does not exist
// or is deleted; every other failure (like missing permissions) is an error.
func (c *Client) GetTunnel(ctx context.Context, tunnelID string) (*shared.CloudflareTunnel, error) {
	t, err := c.api.ZeroTrust.Tunnels.Cloudflared.Get(ctx, tunnelID, zero_trust.TunnelCloudflaredGetParams{
		AccountID: cloudflare.F(c.accountID),
	})
	var apiErr *cloudflare.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !t.DeletedAt.IsZero() {
		return nil, nil
	}
	return t, nil
}

func (c *Client) CreateTunnel(ctx context.Context, name string, secret []byte) (*shared.CloudflareTunnel, error) {
	return c.api.ZeroTrust.Tunnels.Cloudflared.New(ctx, zero_trust.TunnelCloudflaredNewParams{
		AccountID:    cloudflare.F(c.accountID),
		Name:         cloudflare.F(name),
		TunnelSecret: cloudflare.F(base64.StdEncoding.EncodeToString(secret)),
		ConfigSrc:    cloudflare.F(zero_trust.TunnelCloudflaredNewParamsConfigSrcLocal),
	})
}

func (c *Client) DeleteTunnel(ctx context.Context, tunnelID string) error {
	// a tunnel with active connections can't be deleted
	_, err := c.api.ZeroTrust.Tunnels.Cloudflared.Connections.Delete(ctx, tunnelID, zero_trust.TunnelCloudflaredConnectionDeleteParams{
		AccountID: cloudflare.F(c.accountID),
	})
	if err != nil {
		c.log.Warn().Err(err).Str("tunnelId", tunnelID).Msg("cleanup tunnel connections")
	}
	_, err = c.api.ZeroTrust.Tunnels.Cloudflared.Delete(ctx, tunnelID, zero_trust.TunnelCloudflaredDeleteParams{
		AccountID: cloudflare.F(c.accountID),
	})
	if err != nil {
		return err
	}
	c.routedLock.Lock()
	defer c.routedLock.Unlock()
	for host, r := range c.routed {
		if r.tunnelID == tunnelID {
			delete(c.routed, host)
		}
	}
	return nil
}

func normalizeHostname(hostname string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(hostname), "."))
}

func TunnelTarget(tunnelID string) string {
	return tunnelID + ".cfargotunnel.com"
}

func (c *Client) RouteDNS(ctx context.Context, tunnelID string, hostname string) error {
	hostname = normalizeHostname(hostname)
	if hostname == "" {
		return fmt.Errorf("empty hostname")
	}
	c.routedLock.Lock()
	r, found := c.routed[hostname]
	c.routedLock.Unlock()
	if found && r.tunnelID == tunnelID && c.now().Sub(r.at) < routeCacheTTL {
		return nil
	}

	zone, err := c.zoneFor(ctx, hostname)
	if err != nil {
		return err
	}
	target := TunnelTarget(tunnelID)
	var cname *dns.RecordResponse
	var conflict dns.RecordResponseType
	iter := c.api.DNS.Records.ListAutoPaging(ctx, dns.RecordListParams{
		ZoneID: cloudflare.F(zone.ID),
		Name:   cloudflare.F(dns.RecordListParamsName{Exact: cloudflare.F(hostname)}),
	})
	for iter.Next() {
		rec := iter.Current()
		switch rec.Type {
		case dns.RecordResponseTypeCNAME:
			cname = &rec
		case dns.RecordResponseTypeA, dns.RecordResponseTypeAAAA:
			conflict = rec.Type
		}
	}
	if err := iter.Err(); err != nil {
		return err
	}
	body := dns.CNAMERecordParam{
		Name:    cloudflare.F(hostname),
		Type:    cloudflare.F(dns.CNAMERecordTypeCNAME),
		Content: cloudflare.F(target),
		Proxied: cloudflare.F(true),
		TTL:     cloudflare.F(dns.TTL1),
		Comment: cloudflare.F(dnsComment),
	}
	switch {
	case cname != nil && strings.EqualFold(cname.Content, target) && cname.Proxied:
		c.log.Debug().Str("hostname", hostname).Str("target", target).Msg("dns already routed")
	case cname != nil:
		_, err = c.api.DNS.Records.Update(ctx, cname.ID, dns.RecordUpdateParams{
			ZoneID: cloudflare.F(zone.ID),
			Body:   body,
		})
		if err != nil {
			return err
		}
		c.log.Info().Str("hostname", hostname).Str("from", cname.Content).Str("target", target).Msg("dns route updated")
	case conflict != "":
		return fmt.Errorf("%s has an %s record, not replacing it with a tunnel route", hostname, conflict)
	default:
		_, err = c.api.DNS.Records.New(ctx, dns.RecordNewParams{
			ZoneID: cloudflare.F(zone.ID),
			Body:   body,
		})
		if err != nil {
			return err
		}
		c.log.Info().Str("hostname", hostname).Str("target", target).Msg("dns route created")
	}
	c.routedLock.Lock()
	c.routed[hostname] = routed{tunnelID: tunnelID, at: c.now()}
	c.routedLock.Unlock()
	return nil
}

// MatchZone returns the zone with the longest name that hostname belongs to.
func MatchZone(zs []zones.Zone, hostname string) *zones.Zone {
	var best *zones.Zone
	for i := range zs {
		name := strings.ToLower(zs[i].Name)
		if hostname == name || strings.HasSuffix(hostname, "."+name) {
			if best == nil || len(name) > len(best.Name) {
				best = &zs[i]
			}
		}
	}
	return best
}

func (c *Client) zoneFor(ctx context.Context, hostname string) (*zones.Zone, error) {
	c.zonesLock.Lock()
	defer c.zonesLock.Unlock()
	age := c.now().Sub(c.zonesFetched)
	if c.zones == nil || age > zoneCacheTTL {
		if err := c.refreshZones(ctx); err != nil {
			return nil, err
		}
		age = 0
	}
	if z := MatchZone(c.zones, hostname); z != nil {
		return z, nil
	}
	if age > zoneMissRefresh {
		if err := c.refreshZones(ctx); err != nil {
			return nil, err
		}
		if z := MatchZone(c.zones, hostname); z != nil {
			return z, nil
		}
	}
	names := make([]string, 0, len(c.zones))
	for _, z := range c.zones {
		names = append(names, z.Name)
	}
	sort.Strings(names)
	return nil, fmt.Errorf("no zone for %s, token can see zones: %s", hostname, strings.Join(names, ","))
}

// called with zonesLock held
func (c *Client) refreshZones(ctx context.Context) error {
	zs := []zones.Zone{}
	iter := c.api.Zones.ListAutoPaging(ctx, zones.ZoneListParams{})
	for iter.Next() {
		zs = append(zs, iter.Current())
	}
	if err := iter.Err(); err != nil {
		return err
	}
	c.zones = zs
	c.zonesFetched = c.now()
	c.log.Debug().Int("zones", len(zs)).Msg("refreshed zones")
	return nil
}
