package main

import (
	"strings"
	"testing"

	"dokku-nginx-custom/src/pkg/file_config"
)

func TestBuildLimitReqZoneConfig(t *testing.T) {
	cfg := &file_config.Config{
		LimitReqZones: []file_config.LimitReqZoneConfig{
			{
				Name: "api",
				Key:  "$binary_remote_addr",
				Size: "10m",
				Rate: "10r/s",
			},
			{
				Name: "login",
				Key:  "$binary_remote_addr",
				Size: "5m",
				Rate: "1r/s",
			},
		},
	}

	out, names, err := buildLimitReqZoneConfig("myapp", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if names["api"] != "limit_req_myapp_api" {
		t.Fatalf("expected namespaced api zone, got %q", names["api"])
	}
	if names["login"] != "limit_req_myapp_login" {
		t.Fatalf("expected namespaced login zone, got %q", names["login"])
	}

	wantLines := []string{
		"limit_req_zone $binary_remote_addr zone=limit_req_myapp_api:10m rate=10r/s;",
		"limit_req_zone $binary_remote_addr zone=limit_req_myapp_login:5m rate=1r/s;",
	}
	for _, want := range wantLines {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestBuildLimitConnZoneConfig(t *testing.T) {
	cfg := &file_config.Config{
		LimitConnZones: []file_config.LimitConnZoneConfig{
			{
				Name: "addr",
				Key:  "$binary_remote_addr",
				Size: "10m",
			},
		},
	}

	out, names, err := buildLimitConnZoneConfig("myapp", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if names["addr"] != "limit_conn_myapp_addr" {
		t.Fatalf("expected namespaced addr zone, got %q", names["addr"])
	}

	want := "limit_conn_zone $binary_remote_addr zone=limit_conn_myapp_addr:10m;"
	if !strings.Contains(out, want) {
		t.Fatalf("expected output to contain %q, got:\n%s", want, out)
	}
}

func TestBuildLocationConfig_LimitZones(t *testing.T) {
	cfg := &file_config.Config{
		UserVars: file_config.ConfigVars{},
		SysVars:  file_config.ConfigVars{},
		Vhosts: []file_config.VhostConfig{
			{
				ServerName: "api.example.com",
				Locations: []file_config.LocationConfig{
					{
						Modifier: "",
						Uri:      "/api/",
						Body: `limit_req zone={{ .limit_req_zones.api }} burst=20 nodelay;
limit_conn {{ .limit_conn_zones.addr }} 10;
proxy_pass http://backend;`,
					},
				},
			},
		},
	}

	out, err := buildLocationConfig("myapp", cfg, &locationConfigData{
		limitReqZones:  limitZoneResultingNames{"api": "limit_req_myapp_api"},
		limitConnZones: limitZoneResultingNames{"addr": "limit_conn_myapp_addr"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	loc := out["api.example.com"]
	if !strings.Contains(loc, "limit_req zone=limit_req_myapp_api burst=20 nodelay;") {
		t.Fatalf("expected resolved limit_req zone name, got:\n%s", loc)
	}
	if !strings.Contains(loc, "limit_conn limit_conn_myapp_addr 10;") {
		t.Fatalf("expected resolved limit_conn zone name, got:\n%s", loc)
	}
}
