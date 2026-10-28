package cfclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudflare/cloudflare-go/v7/zones"
	"github.com/rs/zerolog"
)

type fakeRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
}

type fakeCF struct {
	t       *testing.T
	lock    sync.Mutex
	zones   []map[string]string
	records map[string][]fakeRecord // by zone id
	tunnels []map[string]any
	calls   []string
	bodies  map[string]map[string]any
}

func newFakeCF(t *testing.T) (*fakeCF, *Client) {
	f := &fakeCF{t: t, records: map[string][]fakeRecord{}, bodies: map[string]map[string]any{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	log := zerolog.New(os.Stderr).Level(zerolog.WarnLevel)
	return f, New(srv.URL, "token", "acc", &log)
}

// 2 items per page, like the API the client must follow the pages
func page[T any](items []T, r *http.Request) []T {
	p, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || p < 1 {
		p = 1
	}
	start := (p - 1) * 2
	if start >= len(items) {
		return []T{}
	}
	end := min(start+2, len(items))
	return items[start:end]
}

func reply(w http.ResponseWriter, result any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success": true, "errors": []any{}, "messages": []any{}, "result": result,
	})
}

func (f *fakeCF) countCalls(prefix string) int {
	f.lock.Lock()
	defer f.lock.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (f *fakeCF) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.lock.Lock()
	defer f.lock.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	body := map[string]any{}
	if b, _ := io.ReadAll(r.Body); len(b) > 0 {
		json.Unmarshal(b, &body)
		f.bodies[r.Method+" "+r.URL.Path] = body
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case r.Method == "GET" && r.URL.Path == "/zones":
		reply(w, page(f.zones, r))
	case len(parts) >= 3 && parts[0] == "zones" && parts[2] == "dns_records":
		zone := parts[1]
		switch r.Method {
		case "GET":
			name := r.URL.Query().Get("name.exact")
			out := []fakeRecord{}
			for _, rec := range f.records[zone] {
				if rec.Name == name {
					out = append(out, rec)
				}
			}
			reply(w, page(out, r))
		case "POST":
			rec := fakeRecord{ID: fmt.Sprintf("rec%d", len(f.calls)), Name: body["name"].(string), Type: body["type"].(string), Content: body["content"].(string), Proxied: body["proxied"].(bool)}
			f.records[zone] = append(f.records[zone], rec)
			reply(w, rec)
		case "PUT":
			for i, rec := range f.records[zone] {
				if rec.ID == parts[3] {
					f.records[zone][i].Content = body["content"].(string)
					f.records[zone][i].Proxied = body["proxied"].(bool)
					reply(w, f.records[zone][i])
					return
				}
			}
			w.WriteHeader(404)
		}
	case strings.HasSuffix(r.URL.Path, "/cfd_tunnel") && r.Method == "GET":
		name := r.URL.Query().Get("name")
		out := []map[string]any{}
		for _, t := range f.tunnels {
			if t["name"] == name {
				out = append(out, t)
			}
		}
		reply(w, page(out, r))
	case strings.HasSuffix(r.URL.Path, "/cfd_tunnel") && r.Method == "POST":
		t := map[string]any{"id": "11111111-2222-3333-4444-555555555555", "name": body["name"]}
		f.tunnels = append(f.tunnels, t)
		reply(w, t)
	case strings.Contains(r.URL.Path, "/cfd_tunnel/") && r.Method == "GET":
		id := parts[len(parts)-1]
		switch id {
		case "forbidden":
			w.WriteHeader(403)
			json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []any{map[string]any{"code": 10000, "message": "Authentication error"}}})
		case "missing":
			w.WriteHeader(404)
			json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []any{map[string]any{"code": 1003, "message": "not found"}}})
		case "deleted":
			reply(w, map[string]any{"id": id, "deleted_at": "2026-01-01T00:00:00Z"})
		default:
			reply(w, map[string]any{"id": id, "name": "t"})
		}
	case strings.Contains(r.URL.Path, "/cfd_tunnel/") && r.Method == "DELETE":
		reply(w, map[string]any{"id": parts[len(parts)-1]})
	default:
		f.t.Errorf("unexpected call %s %s", r.Method, r.URL.String())
		w.WriteHeader(404)
	}
}

func TestMatchZone(t *testing.T) {
	zs := []zones.Zone{{ID: "1", Name: "co.uk"}, {ID: "2", Name: "example.co.uk"}, {ID: "3", Name: "adviser.com"}}
	for host, want := range map[string]string{
		"www.example.co.uk": "2",
		"example.co.uk":     "2",
		"other.co.uk":       "1",
		"a.b.adviser.com":   "3",
		"adviser.com":       "3",
		"notadviser.com":    "",
	} {
		z := MatchZone(zs, host)
		got := ""
		if z != nil {
			got = z.ID
		}
		if got != want {
			t.Errorf("%s: got zone %q want %q", host, got, want)
		}
	}
}

func TestRouteDNSCreatesAcrossZonePages(t *testing.T) {
	f, c := newFakeCF(t)
	// adviser.com is on the 3rd page
	f.zones = []map[string]string{
		{"id": "z1", "name": "a.com"}, {"id": "z2", "name": "b.com"},
		{"id": "z3", "name": "c.com"}, {"id": "z4", "name": "d.com"},
		{"id": "z5", "name": "adviser.com"},
	}
	err := c.RouteDNS(context.Background(), "tid", "LLM-gw.adviser.com.")
	if err != nil {
		t.Fatal(err)
	}
	recs := f.records["z5"]
	if len(recs) != 1 || recs[0].Name != "llm-gw.adviser.com" || recs[0].Type != "CNAME" ||
		recs[0].Content != "tid.cfargotunnel.com" || !recs[0].Proxied {
		t.Fatalf("unexpected records %+v", recs)
	}
	// cached, no further api calls
	before := len(f.calls)
	if err := c.RouteDNS(context.Background(), "tid", "llm-gw.adviser.com"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != before {
		t.Fatalf("expected no api calls, got %v", f.calls[before:])
	}
}

func TestRouteDNSExistingAndUpdate(t *testing.T) {
	f, c := newFakeCF(t)
	f.zones = []map[string]string{{"id": "z", "name": "adviser.com"}}
	f.records["z"] = []fakeRecord{
		{ID: "r1", Name: "ok.adviser.com", Type: "CNAME", Content: "tid.cfargotunnel.com", Proxied: true},
		{ID: "r2", Name: "old.adviser.com", Type: "CNAME", Content: "other.cfargotunnel.com", Proxied: true},
		{ID: "r3", Name: "a.adviser.com", Type: "A", Content: "1.2.3.4"},
	}
	if err := c.RouteDNS(context.Background(), "tid", "ok.adviser.com"); err != nil {
		t.Fatal(err)
	}
	if n := f.countCalls("POST") + f.countCalls("PUT"); n != 0 {
		t.Fatalf("existing route must not be written, got %v", f.calls)
	}
	if err := c.RouteDNS(context.Background(), "tid", "old.adviser.com"); err != nil {
		t.Fatal(err)
	}
	if f.records["z"][1].Content != "tid.cfargotunnel.com" {
		t.Fatalf("not updated %+v", f.records["z"][1])
	}
	err := c.RouteDNS(context.Background(), "tid", "a.adviser.com")
	if err == nil || !strings.Contains(err.Error(), "A record") {
		t.Fatalf("expected conflict error, got %v", err)
	}
	if f.records["z"][2].Content != "1.2.3.4" {
		t.Fatal("A record was changed")
	}
}

func TestRouteDNSUnknownZoneRefreshIsThrottled(t *testing.T) {
	f, c := newFakeCF(t)
	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }
	f.zones = []map[string]string{{"id": "z", "name": "adviser.com"}}
	for i := 0; i < 5; i++ {
		err := c.RouteDNS(context.Background(), "tid", "x.unknown.org")
		if err == nil || !strings.Contains(err.Error(), "adviser.com") {
			t.Fatalf("expected no zone error listing known zones, got %v", err)
		}
	}
	if n := f.countCalls("GET /zones"); n != 2 { // page 1 + empty page 2
		t.Fatalf("zones listed %d times", n)
	}
	// new zone shows up, after the throttle it is found
	f.lock.Lock()
	f.zones = append(f.zones, map[string]string{"id": "z2", "name": "unknown.org"})
	f.lock.Unlock()
	now = now.Add(2 * zoneMissRefresh)
	if err := c.RouteDNS(context.Background(), "tid", "x.unknown.org"); err != nil {
		t.Fatal(err)
	}
}

func TestTunnelLifecycle(t *testing.T) {
	f, c := newFakeCF(t)
	tun, err := c.FindTunnel(context.Background(), "k8s/default/t")
	if err != nil || tun != nil {
		t.Fatalf("expected no tunnel, got %v %v", tun, err)
	}
	created, err := c.CreateTunnel(context.Background(), "k8s/default/t", []byte{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	body := f.bodies["POST /accounts/acc/cfd_tunnel"]
	if body["tunnel_secret"] != "AQID" || body["config_src"] != "local" || body["name"] != "k8s/default/t" {
		t.Fatalf("unexpected create body %v", body)
	}
	tun, err = c.FindTunnel(context.Background(), "k8s/default/t")
	if err != nil || tun == nil || tun.ID != created.ID {
		t.Fatalf("expected tunnel %s, got %v %v", created.ID, tun, err)
	}
	if err := c.DeleteTunnel(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	if f.countCalls("DELETE /accounts/acc/cfd_tunnel/"+created.ID) != 2 { // connections + tunnel
		t.Fatalf("unexpected calls %v", f.calls)
	}
}

func TestGetTunnel(t *testing.T) {
	_, c := newFakeCF(t)
	if _, err := c.GetTunnel(context.Background(), "forbidden"); err == nil {
		t.Fatal("403 must be an error")
	}
	for _, id := range []string{"missing", "deleted"} {
		tun, err := c.GetTunnel(context.Background(), id)
		if err != nil || tun != nil {
			t.Fatalf("%s: expected gone, got %v %v", id, tun, err)
		}
	}
	tun, err := c.GetTunnel(context.Background(), "alive")
	if err != nil || tun == nil || tun.ID != "alive" {
		t.Fatalf("expected tunnel, got %v %v", tun, err)
	}
}
