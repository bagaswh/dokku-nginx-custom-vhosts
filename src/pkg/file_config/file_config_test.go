package file_config

import (
	"strings"
	"testing"
)

func TestLimitZones_ParseAndValidate(t *testing.T) {
	minimalVhost := `
vhosts:
  - server_name: example.com
    locations:
      - modifier: ""
        uri: "/"
        body: |
          return 200;
`

	t.Run("ParsesLimitReqAndConnZones", func(t *testing.T) {
		y := []byte(minimalVhost + `
limit_req_zones:
  - name: api
    key: $binary_remote_addr
    size: 10m
    rate: 10r/s
limit_conn_zones:
  - name: addr
    key: $binary_remote_addr
    size: 10m
`)
		cfg, _, err := ReadConfigBytes(y)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cfg.LimitReqZones) != 1 {
			t.Fatalf("expected 1 limit_req_zones, got %d", len(cfg.LimitReqZones))
		}
		z := cfg.LimitReqZones[0]
		if z.Name != "api" || z.Key != "$binary_remote_addr" || z.Size != "10m" || z.Rate != "10r/s" {
			t.Fatalf("unexpected limit_req zone: %+v", z)
		}
		if len(cfg.LimitConnZones) != 1 {
			t.Fatalf("expected 1 limit_conn_zones, got %d", len(cfg.LimitConnZones))
		}
		c := cfg.LimitConnZones[0]
		if c.Name != "addr" || c.Key != "$binary_remote_addr" || c.Size != "10m" {
			t.Fatalf("unexpected limit_conn zone: %+v", c)
		}
	})

	t.Run("RejectsLimitReqZoneMissingRate", func(t *testing.T) {
		y := []byte(minimalVhost + `
limit_req_zones:
  - name: api
    key: $binary_remote_addr
    size: 10m
`)
		_, _, err := ReadConfigBytes(y)
		if err == nil {
			t.Fatal("expected validation error for missing rate")
		}
		if !strings.Contains(err.Error(), "rate") {
			t.Fatalf("expected error to mention rate, got: %v", err)
		}
	})

	t.Run("RejectsLimitConnZoneMissingKey", func(t *testing.T) {
		y := []byte(minimalVhost + `
limit_conn_zones:
  - name: addr
    size: 10m
`)
		_, _, err := ReadConfigBytes(y)
		if err == nil {
			t.Fatal("expected validation error for missing key")
		}
		if !strings.Contains(err.Error(), "key") {
			t.Fatalf("expected error to mention key, got: %v", err)
		}
	})
}


func TestNullableUpstreamZone_Unmarshal(t *testing.T) {
	t.Run("AbsentZoneField", func(t *testing.T) {
		y := []byte(`
vhosts:
  - server_name: example.com
    locations:
      - modifier: ""
        uri: "/"
        body: |
          return 200;
upstreams:
  - name: api
    servers:
      - addr: "127.0.0.1:5000"
        flags: {}
`)
		cfg, _, err := ReadConfigBytes(y)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cfg.Upstreams) != 1 {
			t.Fatalf("expected 1 upstream, got %d", len(cfg.Upstreams))
		}
		if cfg.Upstreams[0].Zone.IsSet {
			t.Fatalf("expected Zone.IsSet=false for absent field")
		}
	})

	t.Run("ExplicitNullZone", func(t *testing.T) {
		y := []byte(`
vhosts:
  - server_name: example.com
    locations:
      - modifier: ""
        uri: "/"
        body: |
          return 200;
upstreams:
  - name: api
    zone: null
    servers:
      - addr: "127.0.0.1:5000"
        flags: {}
`)
		cfg, _, err := ReadConfigBytes(y)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cfg.Upstreams[0].Zone.IsSet || !cfg.Upstreams[0].Zone.IsNull {
			t.Fatalf("expected zone to be set and null; got IsSet=%v IsNull=%v", cfg.Upstreams[0].Zone.IsSet, cfg.Upstreams[0].Zone.IsNull)
		}
	})

	t.Run("ZoneObject", func(t *testing.T) {
		y := []byte(`
vhosts:
  - server_name: example.com
    locations:
      - modifier: ""
        uri: "/"
        body: |
          return 200;
upstreams:
  - name: api
    zone:
      size: 128k
    servers:
      - addr: "127.0.0.1:5000"
        flags: {}
`)
		cfg, _, err := ReadConfigBytes(y)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cfg.Upstreams[0].Zone.IsSet || cfg.Upstreams[0].Zone.IsNull {
			t.Fatalf("expected zone to be set and not null; got IsSet=%v IsNull=%v", cfg.Upstreams[0].Zone.IsSet, cfg.Upstreams[0].Zone.IsNull)
		}
		if cfg.Upstreams[0].Zone.Value.Size != "128k" {
			t.Fatalf("expected zone size 128k, got %q", cfg.Upstreams[0].Zone.Value.Size)
		}
	})
}
