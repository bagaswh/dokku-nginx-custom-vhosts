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

func mustEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalln("missing required env var:", name)
	}
	return value
}

type upstreamConfigTemplateData struct {
	ProxyUpstreamPorts []string `json:"ProxyUpstreamPorts"`
	AppListeners       []string `json:"AppListeners"`
	App                string   `json:"App"`
}

type upstreamServer struct {
	Addr        string            `json:"addr"`
	Flags       map[string]string `json:"flags"`
	FlagsString string            `json:"flagsString"`
}

type upstreamConfig struct {
	GeneratedUpstreamName string           `json:"generatedUpstreamName"`
	Servers               []upstreamServer `json:"servers"`
}

type upstreamResultingNames map[string]string

func buildUpstreamConfig(appName string, config *file_config.Config, data *upstreamConfigTemplateData) (string, upstreamResultingNames, error) {
	upstreamConfigs := make(map[string]*upstreamConfig, 0)

	upstreamResultingNames := make(upstreamResultingNames, 0)

	// default upstreams
	for _, port := range data.ProxyUpstreamPorts {
		refName := fmt.Sprintf("default-%s", port)
		generatedUpstreamName := fmt.Sprintf("%s-%s", appName, port)
		upstreamResultingNames[refName] = generatedUpstreamName

		if _, ok := upstreamResultingNames["default"]; !ok {
			upstreamResultingNames["default"] = generatedUpstreamName
		}

		upstreamMapKey := fmt.Sprintf("default-%s", port)
		upstreamConfigs[upstreamMapKey] = &upstreamConfig{
			GeneratedUpstreamName: generatedUpstreamName,
			Servers:               make([]upstreamServer, 0),
		}
		uc := upstreamConfigs[upstreamMapKey]
		for _, listener := range data.AppListeners {
			listenerSplit := strings.Split(listener, ":")
			if len(listenerSplit) != 2 {
				fmt.Printf("[warn] failed to parse listener %s\n", listener)
				continue
			}
			upstreamAddr := listenerSplit[0]
			uc.Servers = append(uc.Servers, upstreamServer{
				Addr: fmt.Sprintf("%s:%s", upstreamAddr, port),
			})
			upstreamConfigs[upstreamMapKey] = uc
		}
	}

	// user-supplied upstreams
	for _, upstream := range config.Upstreams {
		if upstream.Name == "" {
			continue
		}
		generatedUpstreamName := fmt.Sprintf("%s-%s", appName, upstream.Name)
		upstreamConfigs[upstream.Name] = &upstreamConfig{
			GeneratedUpstreamName: generatedUpstreamName,
		}
		upstreamResultingNames[upstream.Name] = generatedUpstreamName
		uc := upstreamConfigs[upstream.Name]
		uc.Servers = make([]upstreamServer, 0)
		for _, server := range upstream.Servers {
			uc.Servers = append(uc.Servers, upstreamServer{
				Addr:  server.Addr,
				Flags: server.Flags,
			})
		}
	}

	for _, upstreamCfg := range config.Upstreams {
		var ucs []*upstreamConfig
		if !upstreamCfg.SelectDefault {
			continue
		}

		if upstreamCfg.SelectDefaultPort != 0 {
			uc, ok := upstreamConfigs[fmt.Sprintf("default-%d", upstreamCfg.SelectDefaultPort)]
			if !ok {
				return "", nil, fmt.Errorf("failed to find upstream config for port %d", upstreamCfg.SelectDefaultPort)
			}
			ucs = append(ucs, uc)
		} else {
			for _, uc := range upstreamConfigs {
				ucs = append(ucs, uc)
			}
		}

		if upstreamCfg.DefaultServersFlags != nil {
			for _, serverFlagCfg := range upstreamCfg.DefaultServersFlags {
				for _, uc := range ucs {
					if serverFlagCfg.Selector == "" {
						for i := range uc.Servers {
							// empty selector field means all servers apply
							mergo.Merge(&uc.Servers[i].Flags, serverFlagCfg.Flags, mergo.WithOverride)
						}
					} else {
						for i, server := range uc.Servers {
							regex, err := regexp.Compile(serverFlagCfg.Selector)
							if err != nil {
								return "", nil, fmt.Errorf("failed to compile regex: %v", err)
							}
							if regex.MatchString(server.Addr) {
								mergo.Merge(&uc.Servers[i].Flags, serverFlagCfg.Flags, mergo.WithOverride)
							}
						}
					}
				}
			}
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

	fmt.Println(upstreamConfigs)

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
			// if location.Include != "" {
			// 	continue
			// }

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

func testNginxConfig(nginxTestCommand string) error {
	cmd := exec.Command("sh", "-c", nginxTestCommand)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nginx config test failed: %s", string(output))
	}
	return nil
}

func rollbackToPrevious(nginxConfigDirectory string, previousDir string) error {
	if previousDir == "" {
		// No previous version to rollback to, just remove current symlink
		currentSymlink := path.Join(nginxConfigDirectory, "current")
		if _, err := os.Lstat(currentSymlink); err == nil {
			if err := os.Remove(currentSymlink); err != nil {
				return fmt.Errorf("failed to remove current symlink during rollback: %w", err)
			}
		}
		return nil
	}

	// Rollback to previous version
	return updateCurrentSymlink(nginxConfigDirectory, previousDir)
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

	required := []string{"app-name", "config-file-path"}

	seen := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { seen[f.Name] = true })
	for _, req := range required {
		if !seen[req] {
			log.Fatalf("missing required -%s argument/flag", req)
		}
	}

	nginxWorkingDirectory = path.Join(dokkuAppDataRootDirectory, fmt.Sprintf("%s-config", mustEnv("PROXY_NAME")))
	nginxConfigDirectory := path.Join(nginxWorkingDirectory, "conf.d")

	cfg, _, err := file_config.ReadConfig(configFilePath)
	if err != nil {
		log.Fatalln("error parsing config file:", err)
	}

	containerLabels := make(map[string]any)
	containerLabelsUnmarshalErr := json.Unmarshal([]byte(os.Getenv("DOKKU_APP_CONTAINER_LABELS")), &containerLabels)
	if containerLabelsUnmarshalErr != nil {
		log.Fatalln("error marshaling container labels:", err)
	}

	cfg.SysVars = file_config.ConfigVars{
		"container_working_dir": os.Getenv("DOKKU_APP_CONTAINER_WORKING_DIR_PATH"),
		"container_labels":      containerLabels,
		"dokku_app_storage_dir": os.Getenv("DOKKU_APP_STORAGE_DIR"),
	}

	userVars, err := resolveUserVars(cfg.UserVars, map[string]any{
		"sys_vars": cfg.SysVars,
	})
	if err != nil {
		log.Fatalln("error resolving user vars:", err)
	}
	cfg.UserVars = userVars

	addHeaderMode := mustEnv("NGINX_ADD_HEADER_MODE")
	allowedAddHeaderModes := []string{"add_header", "more_set_headers"}
	if !slices.Contains(allowedAddHeaderModes, addHeaderMode) {
		log.Fatalln("NGINX_ADD_HEADER_MODE must be one of:", allowedAddHeaderModes)
	}

	tmplFuncs := map[string]any{
		"nginx_add_header": func(header string, value string) string {
			if addHeaderMode == "add_header" {
				return fmt.Sprintf("add_header %s %s always;", header, value)
			}
			return fmt.Sprintf("more_set_headers '%s: %s';", header, value)
		},
	}
	sigil.Register(tmplFuncs)

	appListeners := strings.Split(mustEnv("DOKKU_APP_LISTENERS"), " ")
	proxyUpstreamPorts := strings.Split(mustEnv("PROXY_UPSTREAM_PORTS"), " ")

	tmplData := upstreamConfigTemplateData{
		App:                appName,
		ProxyUpstreamPorts: proxyUpstreamPorts,
		AppListeners:       appListeners,
	}

	upstreamCfgStr, upstreams, err := buildUpstreamConfig(appName, cfg, &tmplData)
	if err != nil {
		log.Fatalln("failed to build upstream config:", err)
	}
	_ = upstreamCfgStr

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
		proxyCacheOnDiskRootPath: mustEnv("PROXY_CACHE_ON_DISK_ROOT_PATH"),
		proxyCacheInMemRootPath:  mustEnv("PROXY_CACHE_IN_MEM_ROOT_PATH"),
		proxyCacheDefaultFlags:   proxyCacheDefaultFlags,
		proxyCacheKeyZoneSize:    mustEnv("PROXY_CACHE_DEFAULT_KEY_ZONE_SIZE"),

		fastcgiOnDiskRootPath: mustEnv("FASTCGI_CACHE_ON_DISK_ROOT_PATH"),
		fastcgiInMemRootPath:  mustEnv("FASTCGI_CACHE_IN_MEM_ROOT_PATH"),
		fastcgiDefaultFlags:   fastcgiCacheDefaultFlags,
		fastcgiKeyZoneSize:    mustEnv("FASTCGI_CACHE_DEFAULT_KEY_ZONE_SIZE"),
	}

	proxyCacheCfgStr, proxyCaches, err := buildProxyCacheConfig(appName, buildProxyCacheConfigData, cfg)
	if err != nil {
		log.Fatalln("failed to build proxy cache config:", err)
	}

	fastcgiCacheCfgStr, fastcgiCaches, err := buildFastcgiCacheConfig(appName, buildProxyCacheConfigData, cfg)
	if err != nil {
		log.Fatalln("failed to build fastcgi cache config:", err)
	}

	mapCfgStr, mapResultingVariables, err := buildMapConfig(appName, cfg)
	if err != nil {
		log.Fatalln("failed to build map config:", err)
	}

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
		if err := testNginxConfig(nginxTestCommand); err != nil {
			log.Printf("nginx config test failed, rolling back: %v", err)

			// if rollbackErr := rollbackToPrevious(nginxConfigDirectory, previousDir); rollbackErr != nil {
			// 	log.Fatalln("failed to rollback to previous version:", rollbackErr)
			// }

			log.Fatalln("nginx config test failed:", err)
		}
	}
	log.Println("nginx configuration deployed successfully")
}
