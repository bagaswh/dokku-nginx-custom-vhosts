package main

import (
	"strings"
	"testing"

	"dokku-nginx-custom/src/pkg/file_config"
)

func TestBuildUpstreamConfig_DefaultUpstreamOverrides(t *testing.T) {
	cfg := &file_config.Config{
		UserVars: file_config.ConfigVars{},
		SysVars:  file_config.ConfigVars{},
		UpstreamOverrides: []file_config.UpstreamOverride{
			{
				SelectProcessType: "web",
				SelectPort:        "5000",
				Directives:        []string{"keepalive 32"},
				ServerOverrides: []file_config.UpstreamServerOverride{
					{
						Selector:     ".*",
						DisableFlags: []string{"resolve"},
					},
				},
				Zone: file_config.NullableUpstreamZone{
					IsSet:  true,
					IsNull: true,
				},
			},
		},
	}

	data := &upstreamConfigTemplateData{
		UpstreamPorts: []string{"5000"},
		AppListeners: map[string][]string{
			"web": {"10.0.0.1"},
		},
		App: "myapp",
	}

	out, _, err := buildUpstreamConfig("myapp", cfg, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// zone disabled
	if strings.Contains(out, "zone ") {
		t.Fatalf("expected no zone directive, got: %s", out)
	}

	// keepalive directive present
	if !strings.Contains(out, "keepalive 32;") {
		t.Fatalf("expected keepalive directive, got: %s", out)
	}

	// resolve should be absent because it was disabled
	if strings.Contains(out, " resolve") || strings.Contains(out, "resolve ") {
		t.Fatalf("expected resolve to be disabled, got: %s", out)
	}
}

func TestBuildUpstreamConfig_DefaultUpstreamOverrides_ZoneDisabledRequiresResolveDisabled(t *testing.T) {
	cfg := &file_config.Config{
		UserVars: file_config.ConfigVars{},
		SysVars:  file_config.ConfigVars{},
		UpstreamOverrides: []file_config.UpstreamOverride{
			{
				SelectProcessType: "web",
				SelectPort:        "5000",
				Zone: file_config.NullableUpstreamZone{
					IsSet:  true,
					IsNull: true,
				},
			},
		},
	}

	data := &upstreamConfigTemplateData{
		UpstreamPorts: []string{"5000"},
		AppListeners: map[string][]string{
			"web": {"10.0.0.1"},
		},
		App: "myapp",
	}

	_, _, err := buildUpstreamConfig("myapp", cfg, data)
	if err == nil {
		t.Fatalf("expected error when zone is disabled but resolve isn't disabled")
	}
	if !strings.Contains(err.Error(), "zone disabled") && !strings.Contains(err.Error(), "zone-dependent") {
		t.Fatalf("unexpected error: %v", err)
	}
}

