package file_config

import (
	"testing"
)

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
