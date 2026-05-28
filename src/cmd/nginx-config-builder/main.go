package main

import (
	"dokku-nginx-custom/src/pkg/file_config"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"dario.cat/mergo"
	"github.com/gliderlabs/sigil"
	_ "github.com/gliderlabs/sigil/builtin"
)

var environs []string

func mustEnv(name string) string {
	if environs == nil {
		environs = os.Environ()
	}

	for _, env := range environs {
		split := strings.Split(env, "=")
		var key, value string
		if len(split) == 1 {
			key = env
		}
		if len(split) == 2 {
			key = split[0]
			value = split[1]
		}
		if name == key {
			return value
		}
	}

	log.Fatalln("missing required env var:", name)
	return ""
}

func mustEnvs(names ...string) {
	for _, name := range names {
		_ = mustEnv(name)
	}
}

func envMustNonEmpty(name string) string {
	env := mustEnv(name)
	if env == "" {
		log.Fatalf("missing required value for env %s\n", name)
	}
	return env
}

type upstreamConfigTemplateData struct {
	UpstreamPorts []string            `json:"UpstreamPorts"`
	AppListeners  map[string][]string `json:"AppListeners"`
	App           string              `json:"App"`
}

type upstreamServer struct {
	Addr         string            `json:"addr"`
	Flags        map[string]string `json:"flags"`
	FlagsString  string            `json:"flagsString"`
	DisableFlags []string          `json:"disableFlags"`
	Listener     string            `json:"listener"`
}

type upstreamConfig struct {
	GeneratedUpstreamName string           `json:"generatedUpstreamName"`
	Servers               []upstreamServer `json:"servers"`
	Directives            []string         `json:"directives"`
	ZoneLine              string           `json:"zoneLine"`
}

type upstreamResultingNames map[string]string

func buildUpstreamConfig(appName string, config *file_config.Config, data *upstreamConfigTemplateData) (string, upstreamResultingNames, error) {
	upstreamConfigs := make(map[string]*upstreamConfig, 0)

	upstreamResultingNames := make(upstreamResultingNames, 0)

	zoneDefaultSize := "64k"

	overrideKey := func(processType string, port string) string {
		return fmt.Sprintf("%s-%s", processType, port)
	}

	overridesByRefName := make(map[string][]file_config.UpstreamOverride)
	for _, o := range config.UpstreamOverrides {
		ref := overrideKey(o.SelectProcessType, o.SelectPort)
		overridesByRefName[ref] = append(overridesByRefName[ref], o)
	}

	upstreamZoneNameFor := func(generatedUpstreamName string) string {
		// Must be unique across upstream blocks. Use a deterministic name derived
		// from upstream name so we don't accidentally collide.
		return fmt.Sprintf("%s_zone", generatedUpstreamName)
	}

	containsString := func(list []string, s string) bool {
		for _, v := range list {
			if v == s {
				return true
			}
		}
		return false
	}

	isZoneRequiredByServer := func(flags map[string]string, disableFlags []string) bool {
		// Today, we only enforce the officially-supported shared-memory requirement
		// for `resolve`.
		if containsString(disableFlags, "resolve") {
			return false
		}
		_, hasResolve := flags["resolve"]
		return hasResolve
	}

	removeFlag := func(flags map[string]string, k string) {
		if flags == nil {
			return
		}
		delete(flags, k)
	}

	applyDisableFlags := func(flags map[string]string, disableFlags []string) {
		for _, k := range disableFlags {
			removeFlag(flags, k)
		}
	}

	uniqueAppend := func(dst []string, src ...string) []string {
		for _, s := range src {
			if s == "" {
				continue
			}
			if !containsString(dst, s) {
				dst = append(dst, s)
			}
		}
		return dst
	}

	applyZoneConfig := func(refName string, generatedUpstreamName string, zone file_config.NullableUpstreamZone) (enabled bool, zoneLine string, err error) {
		enabled = true
		zoneName := upstreamZoneNameFor(generatedUpstreamName)
		zoneSize := zoneDefaultSize
		if zone.IsSet {
			if zone.IsNull {
				enabled = false
				return enabled, "", nil
			}
			if zone.Value.Name != "" {
				zoneName = zone.Value.Name
			}
			if zone.Value.Size != "" {
				zoneSize = zone.Value.Size
			}
		}
		return enabled, fmt.Sprintf("zone %s %s;", zoneName, zoneSize), nil
	}

	compileServerOverrideMatchers := func(refName string, overrides []file_config.UpstreamOverride) ([]*regexp.Regexp, []file_config.UpstreamServerOverride, error) {
		matchers := make([]*regexp.Regexp, 0)
		serverOverrides := make([]file_config.UpstreamServerOverride, 0)
		for oi, o := range overrides {
			for si, so := range o.ServerOverrides {
				re, err := regexp.Compile(so.Selector)
				if err != nil {
					return nil, nil, fmt.Errorf("invalid upstream_overrides entry #%d for %q: invalid server_overrides #%d selector %q: %w", oi, refName, si, so.Selector, err)
				}
				matchers = append(matchers, re)
				serverOverrides = append(serverOverrides, so)
			}
		}
		return matchers, serverOverrides, nil
	}

	normalizeUpstreamDirectives := func(raw []string) ([]string, error) {
		out := make([]string, 0, len(raw))
		for _, d := range raw {
			d = strings.TrimSpace(d)
			if d == "" {
				continue
			}
			// `zone` must be configured via schema, not raw directives.
			if strings.HasPrefix(d, "zone ") || d == "zone" || strings.HasPrefix(d, "zone\t") {
				return nil, fmt.Errorf("upstream directive %q is not allowed; use upstream.zone schema instead", d)
			}
			dOut, err := sigil.Execute([]byte(d), map[string]any{"vars": config.UserVars, "sys_vars": config.SysVars}, "upstream_directive")
			if err != nil {
				return nil, fmt.Errorf("failed to parse upstream directive template %q: %w", d, err)
			}
			rendered := strings.TrimSpace(dOut.String())
			if rendered == "" {
				continue
			}
			if !strings.HasSuffix(rendered, ";") {
				rendered += ";"
			}
			out = append(out, rendered)
		}
		return out, nil
	}

	// default upstreams
	for _, port := range data.UpstreamPorts {
		for processType, listeners := range data.AppListeners {
			if len(listeners) == 0 {
				continue
			}

			refName := fmt.Sprintf("%s-%s", processType, port)
			generatedUpstreamName := fmt.Sprintf("%s-%s", appName, refName)
			upstreamResultingNames[refName] = generatedUpstreamName
			if processType == "web" {
				refNameDefault := fmt.Sprintf("default-%s", port)
				upstreamResultingNames[refNameDefault] = generatedUpstreamName

				if _, ok := upstreamResultingNames["default"]; !ok {
					upstreamResultingNames["default"] = generatedUpstreamName
				}
			}

			uc, ok := upstreamConfigs[refName]
			if !ok {
				zoneEnabled, zoneLine, err := applyZoneConfig(refName, generatedUpstreamName, file_config.NullableUpstreamZone{})
				if err != nil {
					return "", nil, err
				}

				directives := make([]string, 0)
				overrideList := overridesByRefName[refName]
				if len(overrideList) > 0 {
					// Apply upstream-level overrides in order.
					for _, o := range overrideList {
						if o.Zone.IsSet {
							zoneEnabled, zoneLine, err = applyZoneConfig(refName, generatedUpstreamName, o.Zone)
							if err != nil {
								return "", nil, err
							}
						}
						if len(o.Directives) > 0 {
							d, err := normalizeUpstreamDirectives(o.Directives)
							if err != nil {
								return "", nil, fmt.Errorf("invalid upstream_overrides for %q directives: %w", refName, err)
							}
							directives = append(directives, d...)
						}
					}
				}

				// Default behavior: zone is enabled unless overridden, with default size.
				// If zone is enabled, default-enable resolve on all default-upstream servers.
				upstreamConfigs[refName] = &upstreamConfig{
					GeneratedUpstreamName: generatedUpstreamName,
					Servers:               make([]upstreamServer, 0),
					Directives:            directives,
					ZoneLine:              zoneLine,
				}
				uc = upstreamConfigs[refName]

				_ = zoneEnabled // used implicitly via uc.ZoneLine below
			}

			overrideList := overridesByRefName[refName]
			serverMatchers, serverOverrides, err := compileServerOverrideMatchers(refName, overrideList)
			if err != nil {
				return "", nil, err
			}

			// Determine zone enabled for this upstream based on uc.ZoneLine.
			zoneEnabled := uc.ZoneLine != ""

			for _, listener := range listeners {
				listenerSplit := strings.Split(listener, ":")
				if len(listenerSplit) != 2 && len(listenerSplit) != 1 {
					fmt.Printf("[warn] failed to parse listener %s\n", listener)
					continue
				}
				addr := listenerSplit[0]

				flags := map[string]string{}
				disableFlags := make([]string, 0)

				// Default behavior: `resolve` is enabled for each server unless explicitly disabled.
				// If zone is disabled, this will be rejected unless the user also disables `resolve`.
				flags["resolve"] = ""

				// Apply per-server overrides (by listener match) in order.
				for i := range serverOverrides {
					so := serverOverrides[i]
					if !serverMatchers[i].MatchString(addr) {
						continue
					}
					if so.Flags != nil {
						for k, v := range so.Flags {
							flags[k] = v
						}
					}
					disableFlags = uniqueAppend(disableFlags, so.DisableFlags...)
				}

				// Apply disable flags effects (generic).
				applyDisableFlags(flags, disableFlags)

				// Enforce zone requirements.
				if !zoneEnabled {
					if isZoneRequiredByServer(flags, disableFlags) {
						return "", nil, fmt.Errorf("default upstream %q has zone disabled but server %q still enables zone-dependent flag %q (disable it with server_overrides.disable_flags: ['resolve'] or enable zone)", refName, fmt.Sprintf("%s:%s", addr, port), "resolve")
					}
				}

				uc.Servers = append(uc.Servers, upstreamServer{
					Addr:         fmt.Sprintf("%s:%s", addr, port),
					Flags:        flags,
					DisableFlags: disableFlags,
					Listener:     addr,
				})
			}
		}
	}

	// user-supplied upstreams
	for _, upstream := range config.Upstreams {
		if upstream.Name == "" {
			continue
		}
		generatedUpstreamName := fmt.Sprintf("%s-%s", appName, upstream.Name)

		zoneEnabled := true
		zoneName := upstreamZoneNameFor(generatedUpstreamName)
		zoneSize := zoneDefaultSize
		if upstream.Zone.IsSet {
			if upstream.Zone.IsNull {
				zoneEnabled = false
			} else {
				if upstream.Zone.Value.Name != "" {
					zoneName = upstream.Zone.Value.Name
				}
				if upstream.Zone.Value.Size != "" {
					zoneSize = upstream.Zone.Value.Size
				}
			}
		}

		directives, err := normalizeUpstreamDirectives(upstream.Directives)
		if err != nil {
			return "", nil, fmt.Errorf("invalid upstream %q directives: %w", upstream.Name, err)
		}

		upstreamConfigs[upstream.Name] = &upstreamConfig{
			GeneratedUpstreamName: generatedUpstreamName,
			Directives:            directives,
		}
		upstreamResultingNames[upstream.Name] = generatedUpstreamName
		uc := upstreamConfigs[upstream.Name]
		uc.Servers = make([]upstreamServer, 0)
		for _, server := range upstream.Servers {
			if server.Flags == nil {
				server.Flags = map[string]string{}
			}

			// Default behavior: `resolve` is enabled for each server unless explicitly disabled.
			// If zone is disabled, this will be rejected unless the user also disables `resolve`.
			if !containsString(server.DisableFlags, "resolve") {
				if _, ok := server.Flags["resolve"]; !ok {
					server.Flags["resolve"] = ""
				}
			}
			applyDisableFlags(server.Flags, server.DisableFlags)

			// If zone is disabled, enforce: no zone-dependent server flags can be present.
			if !zoneEnabled {
				if isZoneRequiredByServer(server.Flags, server.DisableFlags) {
					return "", nil, fmt.Errorf("upstream %q has zone disabled but server %q still enables zone-dependent flag %q (disable it with disable_flags: ['resolve'] or enable zone)", upstream.Name, server.Addr, "resolve")
				}
			}

			uc.Servers = append(uc.Servers, upstreamServer{
				Addr:         server.Addr,
				Flags:        server.Flags,
				DisableFlags: server.DisableFlags,
			})
		}

		if zoneEnabled {
			uc.ZoneLine = fmt.Sprintf("zone %s %s;", zoneName, zoneSize)
		} else {
			uc.ZoneLine = ""
		}
	}

	for _, uc := range upstreamConfigs {
		for i, server := range uc.Servers {
			for k, v := range server.Flags {
				flagString := k
				if v != "" {
					flagString = fmt.Sprintf("%s=%s", k, v)
				}
				if uc.Servers[i].FlagsString != "" {
					uc.Servers[i].FlagsString += " "
				}
				flagStringTemplated, err := sigil.Execute([]byte(flagString), map[string]any{"vars": config.UserVars, "sys_vars": config.SysVars}, "flag_string")
				if err != nil {
					return "", nil, fmt.Errorf("failed to parse template: %w", err)
				}
				uc.Servers[i].FlagsString += flagStringTemplated.String()
			}
		}
	}

	templateStr := `{{- range $key, $value := $.upstreamConfigs -}}
upstream {{ $value.GeneratedUpstreamName }} {
{{- if $value.ZoneLine }}
  {{ $value.ZoneLine }}
{{- end }}
{{- range $line := $value.Directives }}
  {{ $line }}
{{- end }}
{{- range $server := $value.Servers }}
  server {{ $server.Addr }} {{- if $server.FlagsString }} {{ $server.FlagsString }}{{ end -}};
{{- end }}
}
{{ end -}}`

	dataRaw := map[string]any{
		"upstreamConfigs": upstreamConfigs,
		"vars":            config.UserVars,
		"sys_vars":        config.SysVars,
	}

	// Deployment-time introspection: log the fully computed upstream model (names, zone,
	// directives, servers, and server flags) before rendering to nginx config text.
	log.Printf("[nginx-config-builder] computed_upstreams=%s", prettyJSON(upstreamConfigs))

	result, err := sigil.Execute([]byte(templateStr), dataRaw, "upstream_config")
	if err != nil {
		log.Fatalln("failed to parse template:", err)
	}

	return result.String(), upstreamResultingNames, nil
}

type mapResultingVariables map[string]string

func buildMapConfig(appName string, config *file_config.Config) (string, mapResultingVariables, error) {
	mapConfigStr := ""

	templateStr := `map {{ $.string }} ${{ $.variable }} {
{{- range $line := $.lines }}
  {{ $line }}
{{- end }}
}
`

	mapResultingVariables := make(mapResultingVariables, 0)

	for _, mapVar := range config.Maps {
		variableName := fmt.Sprintf("%s_%s", appName, mapVar.Variable)

		linesOut, err := sigil.Execute([]byte(mapVar.Lines), map[string]any{"vars": config.UserVars, "sys_vars": config.SysVars}, "map_lines")
		if err != nil {
			return "", nil, fmt.Errorf("failed to parse template: %w", err)
		}

		stringOut, err := sigil.Execute([]byte(mapVar.String), map[string]any{"vars": config.UserVars, "sys_vars": config.SysVars}, "map_string")
		if err != nil {
			return "", nil, fmt.Errorf("failed to parse template: %w", err)
		}

		dataRaw := map[string]any{
			"variable": variableName,
			"string":   stringOut.String(),
			"lines":    strings.Split(linesOut.String(), "\n"),
		}

		result, err := sigil.Execute([]byte(templateStr), dataRaw, "map_config")
		if err != nil {
			return "", nil, fmt.Errorf("failed to parse template: %w", err)
		}
		mapConfigStr += result.String()

		for _, mapVar := range config.Maps {
			mapResultingVariables[mapVar.Variable] = variableName
		}

	}

	return mapConfigStr, mapResultingVariables, nil
}

type buildProxyCacheConfigData struct {
	proxyCacheOnDiskRootPath string
	proxyCacheInMemRootPath  string
	proxyCacheDefaultFlags   map[string]string
	proxyCacheKeyZoneSize    string

	fastcgiOnDiskRootPath string
	fastcgiInMemRootPath  string
	fastcgiDefaultFlags   map[string]string
	fastcgiKeyZoneSize    string
}

type cacheResultingNames map[string]string

func buildProxyCacheConfig(appName string, buildProxyCacheCfgData buildProxyCacheConfigData, config *file_config.Config) (string, cacheResultingNames, error) {
	cacheResultingNames := make(cacheResultingNames, 0)

	cfgStr := ""

	for _, cache := range config.ProxyCaches {
		cacheName := fmt.Sprintf("proxy_%s_%s", appName, cache.Name)
		cachePath := cache.CachePath
		if cachePath == "" {
			if cache.InMem {
				cachePath = path.Join(buildProxyCacheCfgData.proxyCacheInMemRootPath, cacheName)
			} else {
				cachePath = path.Join(buildProxyCacheCfgData.proxyCacheOnDiskRootPath, cacheName)
			}
		}

		flags := buildProxyCacheCfgData.proxyCacheDefaultFlags
		if cache.Flags != nil {
			mergo.Merge(&flags, cache.Flags, mergo.WithOverride)
		}

		keyZoneSize := cache.KeyZoneSize
		if keyZoneSize == "" {
			keyZoneSize = buildProxyCacheCfgData.proxyCacheKeyZoneSize
		}

		cacheResultingNames[cache.Name] = cacheName

		flagStr := ""
		for k, v := range flags {
			str := k
			if v != "" {
				str = fmt.Sprintf("%s=%s", k, v)
			}
			if flagStr != "" {
				flagStr = flagStr + " "
			}
			tmplOut, err := sigil.Execute([]byte(str), map[string]any{"vars": config.UserVars, "sys_vars": config.SysVars}, "proxy_cache_flag_string")
			if err != nil {
				return "", nil, fmt.Errorf("failed to parse template: %w", err)
			}
			flagStr += tmplOut.String()
		}

		if cfgStr != "" {
			cfgStr += "\n"
		}

		cfgStr += fmt.Sprintf("proxy_cache_path %s keys_zone=%s:%s %s;", cachePath, cacheName, keyZoneSize, flagStr)
	}

	return cfgStr, cacheResultingNames, nil
}

func buildFastcgiCacheConfig(appName string, buildProxyCacheCfgData buildProxyCacheConfigData, config *file_config.Config) (string, cacheResultingNames, error) {
	cacheResultingNames := make(cacheResultingNames, 0)

	cfgStr := ""

	for _, cache := range config.FastcgiCaches {
		cacheName := fmt.Sprintf("fastcgi_%s_%s", appName, cache.Name)
		cachePath := cache.CachePath
		if cachePath == "" {
			if cache.InMem {
				cachePath = path.Join(buildProxyCacheCfgData.fastcgiInMemRootPath, cacheName)
			} else {
				cachePath = path.Join(buildProxyCacheCfgData.fastcgiOnDiskRootPath, cacheName)
			}
		}

		flags := buildProxyCacheCfgData.fastcgiDefaultFlags
		if cache.Flags != nil {
			mergo.Merge(&flags, cache.Flags, mergo.WithOverride)
		}

		keyZoneSize := cache.KeyZoneSize
		if keyZoneSize == "" {
			keyZoneSize = buildProxyCacheCfgData.fastcgiKeyZoneSize
		}

		cacheResultingNames[cache.Name] = cacheName

		flagStr := ""
		for k, v := range flags {
			str := k
			if v != "" {
				str = fmt.Sprintf("%s=%s", k, v)
			}
			if flagStr != "" {
				flagStr = flagStr + " "
			}
			tmplOut, err := sigil.Execute([]byte(str), map[string]any{"vars": config.UserVars, "sys_vars": config.SysVars}, "fastcgi_cache_flag_string")
			if err != nil {
				return "", nil, fmt.Errorf("failed to parse template: %w", err)
			}
			flagStr += tmplOut.String()
		}

		if cfgStr != "" {
			cfgStr += "\n"
		}
		cfgStr += fmt.Sprintf("fastcgi_cache_path %s keys_zone=%s:%s %s;", cachePath, cacheName, keyZoneSize, flagStr)
	}

	return cfgStr, cacheResultingNames, nil
}

type locationConfigData struct {
	upstreams     upstreamResultingNames
	mapVariables  mapResultingVariables
	proxyCaches   cacheResultingNames
	fastcgiCaches cacheResultingNames
}

type vhostToLocationConfigStringMap map[string]string

func buildLocationConfig(appName string, config *file_config.Config, data *locationConfigData) (vhostToLocationConfigStringMap, error) {
	locationConfigs := make(vhostToLocationConfigStringMap, 0)

	tmplLocationBlockStr := `{{- if or $.uri $.named -}}
location {{ $.modifier }}{{ if $.named }}@{{ $.named }}{{ else }}{{ $.uri }}{{ end }} {
{{- end -}}
{{- range $line := $.bodyLines }}
  {{ $line }}
{{- end }}
{{- if or $.uri $.named }}
}

{{ end -}}
`

	for _, vhost := range config.Vhosts {
		locationConfigStr := ""

		variableNames := make(map[string]string)
		variables := make(map[string]string)

		for _, variable := range vhost.Variables {
			variableNames[variable.Name] = variable.Name
			variables[variable.Name] = variable.Value
		}

		tmplData := map[string]any{
			"locationConfigs": make(map[string]any),
			"vars":            config.UserVars,
			"sys_vars":        config.SysVars,
		}

		namedLocations := make(map[string]string)
		for _, location := range vhost.Locations {
			if location.Named != "" {
				namedLocations[location.Named] = fmt.Sprintf("%s_%s", appName, location.Named)
			}
		}

		bodyTmplData := map[string]any{
			"map_variables":   data.mapVariables,
			"upstreams":       data.upstreams,
			"proxy_caches":    data.proxyCaches,
			"fastcgi_caches":  data.fastcgiCaches,
			"variables":       variableNames,
			"named_locations": namedLocations,
			"vars":            config.UserVars,
			"sys_vars":        config.SysVars,
		}

		for _, location := range vhost.Locations {

			modifierOut, err := sigil.Execute([]byte(location.Modifier), bodyTmplData, fmt.Sprintf("location_modifier_vhost_%s_uri_%s", vhost.ServerName, location.Uri))
			if err != nil {
				return nil, fmt.Errorf("failed to parse location.Modifier template: %w", err)
			}
			tmplData["modifier"] = modifierOut.String()

			uriOut, err := sigil.Execute([]byte(location.Uri), bodyTmplData, fmt.Sprintf("location_uri_vhost_%s_uri_%s", vhost.ServerName, location.Uri))
			if err != nil {
				return nil, fmt.Errorf("failed to parse location.Uri template: %w", err)
			}
			tmplData["uri"] = uriOut.String()

			bodyOut, err := sigil.Execute([]byte(location.Body), bodyTmplData, fmt.Sprintf("location_body_vhost_%s_uri_%s", vhost.ServerName, location.Uri))
			if err != nil {
				return nil, fmt.Errorf("failed to parse location.Body template: %w", err)
			}
			bodyLines := strings.Split(bodyOut.String(), "\n")
			tmplData["bodyLines"] = bodyLines

			if location.Named != "" {
				tmplData["named"] = namedLocations[location.Named]
			} else {
				tmplData["named"] = ""
			}

			locationOut, err := sigil.Execute([]byte(tmplLocationBlockStr), tmplData, fmt.Sprintf("location_block_vhost_%s_uri_%s", vhost.ServerName, location.Uri))
			if err != nil {
				return nil, fmt.Errorf("failed to parse tmplLocationBlockStr template: %w", err)
			}

			if locationConfigStr != "" {
				locationConfigStr += "\n"
			}
			locationConfigStr += locationOut.String()

		}

		locationConfigs[vhost.ServerName] = locationConfigStr
	}

	return locationConfigs, nil
}

var nginxWorkingDirectory string

func getCurrentConfigVersionDirectory(nginxConfigDirectory string) (string, error) {
	files, err := filepath.Glob(path.Join(nginxConfigDirectory, "release-*"))
	if err != nil {
		return "", fmt.Errorf("failed to read nginx config directory: %w", err)
	}

	yyyymmdd := time.Now().Format("20060102")

	sequence := 1
	for _, file := range files {
		if strings.HasPrefix(file, fmt.Sprintf("%s/release-%s.", nginxConfigDirectory, yyyymmdd)) {
			sequence++
		}
	}

	if len(files) == 0 {
		return fmt.Sprintf("%s/release-%s.1", nginxConfigDirectory, yyyymmdd), nil
	}

	var latestDir string
	var latestDate int
	var latestSequence int

	releasePattern := regexp.MustCompile(`^release-(\d+)\.(\d+)$`)

	for _, file := range files {
		dirName := filepath.Base(file)

		if info, err := os.Stat(file); err != nil || !info.IsDir() {
			continue
		}

		matches := releasePattern.FindStringSubmatch(dirName)
		if len(matches) != 3 {
			continue
		}

		date, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}
		sequence, err := strconv.Atoi(matches[2])
		if err != nil {
			continue
		}

		if latestDir == "" || date > latestDate || (date == latestDate && sequence > latestSequence) {
			latestDir = file
			latestDate = date
			latestSequence = sequence
		}
	}

	if latestDir == "" {
		return "", fmt.Errorf("no valid release directories found")
	}

	return latestDir, nil
}

func getPreviousVersionDirectory(nginxConfigDirectory string) (string, error) {
	currentSymlink := path.Join(nginxConfigDirectory, "current")

	// Check if current symlink exists
	if _, err := os.Lstat(currentSymlink); os.IsNotExist(err) {
		return "", nil // No previous version
	}

	// Resolve the symlink
	previousDir, err := os.Readlink(currentSymlink)
	if err != nil {
		return "", fmt.Errorf("failed to read current symlink: %w", err)
	}

	// Make it absolute if it's relative
	if !path.IsAbs(previousDir) {
		previousDir = path.Join(nginxConfigDirectory, previousDir)
	}

	return previousDir, nil
}

func copyConfigToRelease(configContent string, releaseDir string, filename string) error {
	configPath := path.Join(releaseDir, filename)

	// Create the full directory path including any subdirectories
	configDir := path.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", configDir, err)
	}

	// Write the config content to the file
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", filename, err)
	}

	return nil
}

func updateCurrentSymlink(nginxConfigDirectory string, newReleaseDir string) error {
	currentSymlink := path.Join(nginxConfigDirectory, "current")

	// Remove existing symlink if it exists
	if _, err := os.Lstat(currentSymlink); err == nil {
		if err := os.Remove(currentSymlink); err != nil {
			return fmt.Errorf("failed to remove existing current symlink: %w", err)
		}
	}

	// Create new symlink
	// Use relative path for the symlink
	relPath, err := filepath.Rel(nginxConfigDirectory, newReleaseDir)
	if err != nil {
		return fmt.Errorf("failed to get relative path: %w", err)
	}

	if err := os.Symlink(relPath, currentSymlink); err != nil {
		return fmt.Errorf("failed to create current symlink: %w", err)
	}

	return nil
}

func testNginxConfig(nginxTestCommand ...string) error {
	cmd := exec.Command(nginxTestCommand[0], nginxTestCommand[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nginx config test failed: %s", string(output))
	}
	return nil
}

func resolveUserVars(userVars map[string]any, sysVars map[string]any) (map[string]any, error) {
	resolvedUserVars := make(map[string]any)
	for k, v := range userVars {
		resolved, err := resolveValue(v, sysVars, fmt.Sprintf("user_var_%s", k))
		if err != nil {
			return nil, fmt.Errorf("failed to resolve user var %s: %w", k, err)
		}
		resolvedUserVars[k] = resolved
	}
	return resolvedUserVars, nil
}

func resolveValue(v any, sysVars map[string]any, context string) (any, error) {
	if v == nil {
		return nil, nil
	}

	switch val := v.(type) {
	case string:
		// Resolve string templates
		resolved, err := sigil.Execute([]byte(val), sysVars, context)
		if err != nil {
			return nil, err
		}
		return resolved.String(), nil

	case map[string]any:
		// Recursively resolve nested maps
		resolvedMap := make(map[string]any)
		for k, nestedVal := range val {
			resolved, err := resolveValue(nestedVal, sysVars, fmt.Sprintf("%s.%s", context, k))
			if err != nil {
				return nil, fmt.Errorf("in key %s: %w", k, err)
			}
			resolvedMap[k] = resolved
		}
		return resolvedMap, nil

	case []any:
		// Recursively resolve slices
		resolvedSlice := make([]any, len(val))
		for i, item := range val {
			resolved, err := resolveValue(item, sysVars, fmt.Sprintf("%s[%d]", context, i))
			if err != nil {
				return nil, fmt.Errorf("at index %d: %w", i, err)
			}
			resolvedSlice[i] = resolved
		}
		return resolvedSlice, nil

	default:
		// Return other types as-is (int, bool, float, etc.)
		return v, nil
	}
}

func prettyJSON(v any) string {
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(pretty)
}

func normalizePath(path string) string {
	return filepath.Clean(path)
}

func main() {

	var appName string
	var configFilePath string
	var dokkuAppDataRootDirectory string
	var nginxTestCommand string
	var withoutNginxTest bool
	flag.StringVar(&appName, "app-name", "", "app name")
	flag.StringVar(&configFilePath, "config-file-path", "", "path to config file")
	flag.StringVar(&dokkuAppDataRootDirectory, "dokku-data-root-directory", "", "dokku data root directory")
	flag.StringVar(&nginxTestCommand, "nginx-test-command", "nginx -t", "nginx test command")
	flag.BoolVar(&withoutNginxTest, "without-nginx-test", false, "do not run nginx test")

	flag.Parse()

	nginxTestCommandSplit := strings.Split(nginxTestCommand, " ")

	required := []string{"app-name", "config-file-path"}

	seen := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { seen[f.Name] = true })
	for _, req := range required {
		if !seen[req] {
			log.Fatalf("missing required -%s argument/flag", req)
		}
	}

	mustEnvs(
		"PROXY_NAME",
		"DOKKU_APP_CONTAINER_LABELS",
		"DOKKU_APP_CONTAINER_MOUNTS",
		"DOKKU_APP_LISTENERS",
		"PROXY_UPSTREAM_PORTS",
		"PROXY_CACHE_DEFAULT_FLAGS",
		"FASTCGI_CACHE_DEFAULT_FLAGS",
		"PROXY_CACHE_ON_DISK_ROOT_PATH",
		"PROXY_CACHE_IN_MEM_ROOT_PATH",
		"FASTCGI_CACHE_ON_DISK_ROOT_PATH",
		"FASTCGI_CACHE_IN_MEM_ROOT_PATH",
		"PROXY_CACHE_DEFAULT_KEY_ZONE_SIZE",
		"FASTCGI_CACHE_DEFAULT_KEY_ZONE_SIZE",
		"NGINX_ADD_HEADER_MODE",
		"NGINX_ACCESS_LOG_ROOT_DIR",
		"NGINX_ERROR_LOG_ROOT_DIR",
	)

	nginxWorkingDirectory = path.Join(dokkuAppDataRootDirectory, fmt.Sprintf("%s-config", envMustNonEmpty("PROXY_NAME")))
	nginxConfigDirectory := path.Join(nginxWorkingDirectory, "conf.d")

	cfg, _, readConfigFileErr := file_config.ReadConfig(configFilePath)
	if readConfigFileErr != nil {
		log.Fatalln("error parsing config file:", readConfigFileErr)
	}

	containerLabels := make(map[string]any)
	containerLabelsUnmarshalErr := json.Unmarshal([]byte(os.Getenv("DOKKU_APP_CONTAINER_LABELS")), &containerLabels)
	if containerLabelsUnmarshalErr != nil {
		log.Fatalf("error marshaling container labels: %v; labels=%s", containerLabelsUnmarshalErr, os.Getenv("DOKKU_APP_CONTAINER_LABELS"))
	}

	type Mount struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		Mode        string `json:"Mode"`
		RW          bool   `json:"RW"`
		Propagation string `json:"Propagation"`
	}

	var containerMounts []Mount
	containerMountsUnmarshalErr := json.Unmarshal([]byte(os.Getenv("DOKKU_APP_CONTAINER_MOUNTS")), &containerMounts)
	if containerMountsUnmarshalErr != nil {
		log.Fatalln("error marshaling container mounts:", containerMountsUnmarshalErr)
	}

	containerMountsMap := make(map[string]Mount)
	for _, mount := range containerMounts {
		containerMountsMap[mount.Destination] = mount
	}

	cfg.SysVars = file_config.ConfigVars{
		"container_labels": containerLabels,
		"container_mounts": containerMountsMap,
		"app_name":         appName,
	}
	fmt.Printf("[VARDEBUG] SysVars=%s\n", prettyJSON(cfg.SysVars))

	userVars, err := resolveUserVars(cfg.UserVars, map[string]any{
		"sys_vars": cfg.SysVars,
	})
	if err != nil {
		log.Fatalln("error resolving user vars:", err)
	}
	cfg.UserVars = userVars
	fmt.Printf("[VARDEBUG] UserVars=%s\n", prettyJSON(cfg.UserVars))

	addHeaderMode := envMustNonEmpty("NGINX_ADD_HEADER_MODE")
	allowedAddHeaderModes := []string{"add_header", "more_set_headers"}
	if !slices.Contains(allowedAddHeaderModes, addHeaderMode) {
		log.Fatalln("NGINX_ADD_HEADER_MODE must be one of:", allowedAddHeaderModes)
	}
	fmt.Printf("[VARDEBUG] addHeaderMode=%s\n", addHeaderMode)

	tmplFuncs := map[string]any{
		"nginx_add_header": func(header string, value string) string {
			if addHeaderMode == "add_header" {
				return fmt.Sprintf("add_header %s %s always;", header, value)
			}
			return fmt.Sprintf("more_set_headers \"%s: %s\";", header, value)
		},
		"nginx_log": func(params ...string) string {
			if len(params) < 1 {
				panic("nginx_log function requires at least one parameter. If given one parameter, it will be treated as the log type. If given two parameters, the first will be the log type and the second will be the filename. If filename parameter is omitted, it defaults to the <app_name>.log. If given 3 parameters, the 3rd parameter will be the access log format.")
			}

			var typ, filename, accessLogFormat string
			typ = params[0]
			if len(params) == 2 {
				filename = params[1]
			}
			if filename == "" {
				filename = fmt.Sprintf("%s.log", appName)
			}
			if len(params) == 3 {
				accessLogFormat = params[2]
			}
			if accessLogFormat == "" {
				accessLogFormat = os.Getenv("NGINX_DEFAULT_ACCESS_LOG_FORMAT")
			}

			nginxAccessLogRootDir := envMustNonEmpty("NGINX_ACCESS_LOG_ROOT_DIR")
			nginxErrorLogRootDir := envMustNonEmpty("NGINX_ERROR_LOG_ROOT_DIR")

			switch typ {
			case "access":
				return fmt.Sprintf("access_log %s/%s %s;", nginxAccessLogRootDir, filename, accessLogFormat)
			case "error":
				return fmt.Sprintf("error_log %s/%s;", nginxErrorLogRootDir, filename)
			default:
				panic(fmt.Errorf("invalid log type %q", typ))
			}
		},
		"realpath": func(path string) string {
			absPath, err := filepath.Abs(path)
			if err != nil {
				panic(err)
			}
			return absPath
		},
		"container_mount_source_abs": func(mountPath string, joinElem ...string) string {
			mountPath = normalizePath(mountPath)
			p := ""
			if mnt, ok := containerMountsMap[mountPath]; ok {
				p = mnt.Source
			} else {
				panic(fmt.Errorf("container mount %q not found", mountPath))
			}
			if len(joinElem) > 0 {
				elems := append([]string{p}, joinElem...)
				p = filepath.Join(elems...)
				var err error
				p, err = filepath.Abs(p)
				if err != nil {
					panic(err)
				}
			}
			return p
		},
		"normalize_path": normalizePath,
	}
	sigil.Register(tmplFuncs)

	DOKKU_APP_LISTENERS := os.Getenv("DOKKU_APP_LISTENERS")
	var appListeners map[string][]string
	appListenersUnmarshalErr := json.Unmarshal([]byte(DOKKU_APP_LISTENERS), &appListeners)
	if appListenersUnmarshalErr != nil {
		log.Fatalln("error unmarshaling app listeners:", appListenersUnmarshalErr)
	}
	fmt.Printf("[VARDEBUG] appListeners computed=%s\n", prettyJSON(appListeners))
	webListeners, ok := appListeners["web"]
	if ok && len(webListeners) > 0 && webListeners[0] == "invalid" {
		fmt.Printf("[VARDEBUG] invalid IP received, app listeners are empty")
		appListeners = map[string][]string{}
	}
	filteredAppListeners := make(map[string][]string)
	for processType, listeners := range appListeners {
		if len(listeners) == 0 {
			continue
		}
		filteredAppListeners[processType] = listeners
	}
	fmt.Printf("[VARDEBUG] filteredAppListeners=%s\n", prettyJSON(filteredAppListeners))

	tmplData := upstreamConfigTemplateData{
		App:           appName,
		AppListeners:  filteredAppListeners,
		UpstreamPorts: strings.Split(os.Getenv("PROXY_UPSTREAM_PORTS"), " "),
	}

	upstreamCfgStr, upstreams, err := buildUpstreamConfig(appName, cfg, &tmplData)
	if err != nil {
		log.Fatalln("failed to build upstream config:", err)
	}
	_ = upstreamCfgStr
	fmt.Printf("[VARDEBUG] upstreams=%s\n", prettyJSON(upstreams))
	fmt.Printf("[VARDEBUG] upstreamCfgStr=%s\n", upstreamCfgStr)

	proxyCacheDefaultFlags := make(map[string]string)
	for _, flag := range strings.Split(os.Getenv("PROXY_CACHE_DEFAULT_FLAGS"), " ") {
		flagSplit := strings.Split(flag, "=")
		if len(flagSplit) != 2 {
			proxyCacheDefaultFlags[flagSplit[0]] = ""
		} else if len(flagSplit) == 2 {
			proxyCacheDefaultFlags[flagSplit[0]] = flagSplit[1]
		} else {
			log.Fatalln("failed to parse proxy cache default flags:", flag)
		}
	}

	fastcgiCacheDefaultFlags := make(map[string]string)
	for _, flag := range strings.Split(os.Getenv("FASTCGI_CACHE_DEFAULT_FLAGS"), " ") {
		flagSplit := strings.Split(flag, "=")
		if len(flagSplit) != 2 {
			fastcgiCacheDefaultFlags[flagSplit[0]] = ""
		} else if len(flagSplit) == 2 {
			fastcgiCacheDefaultFlags[flagSplit[0]] = flagSplit[1]
		} else {
			log.Fatalln("failed to parse fastcgi cache default flags:", flag)
		}
	}

	buildProxyCacheConfigData := buildProxyCacheConfigData{
		proxyCacheOnDiskRootPath: envMustNonEmpty("PROXY_CACHE_ON_DISK_ROOT_PATH"),
		proxyCacheInMemRootPath:  envMustNonEmpty("PROXY_CACHE_IN_MEM_ROOT_PATH"),
		proxyCacheDefaultFlags:   proxyCacheDefaultFlags,
		proxyCacheKeyZoneSize:    envMustNonEmpty("PROXY_CACHE_DEFAULT_KEY_ZONE_SIZE"),

		fastcgiOnDiskRootPath: envMustNonEmpty("FASTCGI_CACHE_ON_DISK_ROOT_PATH"),
		fastcgiInMemRootPath:  envMustNonEmpty("FASTCGI_CACHE_IN_MEM_ROOT_PATH"),
		fastcgiDefaultFlags:   fastcgiCacheDefaultFlags,
		fastcgiKeyZoneSize:    envMustNonEmpty("FASTCGI_CACHE_DEFAULT_KEY_ZONE_SIZE"),
	}

	proxyCacheCfgStr, proxyCaches, err := buildProxyCacheConfig(appName, buildProxyCacheConfigData, cfg)
	if err != nil {
		log.Fatalln("failed to build proxy cache config:", err)
	}
	fmt.Printf("[VARDEBUG] proxyCaches=%s\n", prettyJSON(proxyCaches))
	fmt.Printf("[VARDEBUG] proxyCacheCfgStr=%s\n", proxyCacheCfgStr)

	fastcgiCacheCfgStr, fastcgiCaches, err := buildFastcgiCacheConfig(appName, buildProxyCacheConfigData, cfg)
	if err != nil {
		log.Fatalln("failed to build fastcgi cache config:", err)
	}
	fmt.Printf("[VARDEBUG] fastcgiCaches=%s\n", prettyJSON(fastcgiCaches))
	fmt.Printf("[VARDEBUG] fastcgiCacheCfgStr=%s\n", fastcgiCacheCfgStr)

	mapCfgStr, mapResultingVariables, err := buildMapConfig(appName, cfg)
	if err != nil {
		log.Fatalln("failed to build map config:", err)
	}
	fmt.Printf("[VARDEBUG] mapCfgStr=%s\n", mapCfgStr)
	fmt.Printf("[VARDEBUG] mapResultingVariables=%s\n", prettyJSON(mapResultingVariables))

	locationConfigs, err := buildLocationConfig(appName, cfg, &locationConfigData{
		upstreams:     upstreams,
		proxyCaches:   proxyCaches,
		fastcgiCaches: fastcgiCaches,
		mapVariables:  mapResultingVariables,
	})
	if err != nil {
		log.Fatalln("failed to build location config:", err)
	}

	latestReleaseDir, err := getCurrentConfigVersionDirectory(nginxConfigDirectory)
	if err != nil {
		log.Fatalln("failed to get latest release directory:", err)
	}

	_, err = getPreviousVersionDirectory(nginxConfigDirectory)
	if err != nil {
		log.Fatalln("failed to get previous version directory:", err)
	}

	configFiles := map[string]string{
		"upstreams.conf":      upstreamCfgStr,
		"proxy_caches.conf":   proxyCacheCfgStr,
		"fastcgi_caches.conf": fastcgiCacheCfgStr,
		"maps.conf":           mapCfgStr,
	}

	for vhost, locationConfig := range locationConfigs {
		configFiles[fmt.Sprintf("vhosts/%s/vhost.conf", vhost)] = locationConfig
		fmt.Printf("[VARDEBUG] location config for vhost %s: %s\n", vhost, locationConfig)
	}

	for filename, content := range configFiles {
		if err := copyConfigToRelease(content, latestReleaseDir, filename); err != nil {
			log.Fatalln("failed to copy config file:", err)
		}
	}

	if err := updateCurrentSymlink(nginxConfigDirectory, latestReleaseDir); err != nil {
		log.Fatalln("failed to update current symlink:", err)
	}

	if !withoutNginxTest {
		log.Printf("performing nginx test with commands: %#v\n", nginxTestCommandSplit)
		if err := testNginxConfig(nginxTestCommandSplit...); err != nil {
			log.Fatalf("nginx config test failed: %v\n", err)
		}
	}
	log.Println("nginx configuration deployed successfully")
}
