package main

import (
	"dokku-nginx-custom/src/pkg/file_config"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func mustEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("missing required env var: %s", name)
	}
	return value
}

func parseCSVFlag(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		// Strip surrounding quotes if present
		trimmed = strings.Trim(trimmed, `"`)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func ensureCacheNameExists(cfgCaches []file_config.CacheConfig, requested []string, kind string) (map[string]file_config.CacheConfig, error) {
	cacheMap := make(map[string]file_config.CacheConfig)
	for _, cache := range cfgCaches {
		cacheMap[cache.Name] = cache
	}

	selected := make(map[string]file_config.CacheConfig)
	for _, name := range requested {
		cacheCfg, ok := cacheMap[name]
		if !ok {
			return nil, fmt.Errorf("%s cache %q not found in config", kind, name)
		}
		selected[name] = cacheCfg
	}

	return selected, nil
}

func buildCachePath(kind string, appName string, cache file_config.CacheConfig, roots map[string]string) (string, error) {
	if cache.CachePath != "" {
		return cache.CachePath, nil
	}

	if appName == "" {
		return "", fmt.Errorf("cache %q: app name is required to derive cache path; set -app-name or APP_NAME/DOKKU_APP_NAME", cache.Name)
	}

	var root string
	switch {
	case cache.InMem:
		root = roots["in_mem"]
	case cache.OnDisk:
		root = roots["on_disk"]
	default:
		// follow builder default: prefer on_disk when unspecified
		root = roots["on_disk"]
	}

	if root == "" {
		return "", fmt.Errorf("cache %q: missing cache root path for %s caches", cache.Name, kind)
	}

	prefix := "proxy"
	if kind == "fastcgi" {
		prefix = "fastcgi"
	}

	cacheDirName := fmt.Sprintf("%s_%s_%s", prefix, appName, cache.Name)
	return filepath.Join(root, cacheDirName), nil
}

func loadCacheRoots(kind string, needed bool) map[string]string {
	if !needed {
		return nil
	}

	if kind == "proxy" {
		return map[string]string{
			"in_mem":  mustEnv("PROXY_CACHE_IN_MEM_ROOT_PATH"),
			"on_disk": mustEnv("PROXY_CACHE_ON_DISK_ROOT_PATH"),
		}
	}

	return map[string]string{
		"in_mem":  mustEnv("FASTCGI_CACHE_IN_MEM_ROOT_PATH"),
		"on_disk": mustEnv("FASTCGI_CACHE_ON_DISK_ROOT_PATH"),
	}
}

func purgeCacheDir(path string, purgeCommand string) error {
	if path == "" || path == "/" {
		return fmt.Errorf("refusing to remove unsafe path %q", path)
	}
	if purgeCommand == "" {
		return os.RemoveAll(path)
	}
	purgeCommands := strings.Split(purgeCommand, " ")
	purgeCommands = append(purgeCommands, path)
	cmd := exec.Command(purgeCommands[0], purgeCommands[1:]...)
	log.Printf("Running purge command: %s\n", cmd.String())
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to purge cache dir %s: %w: %s", path, err, string(output))
	}
	return nil
}

func main() {
	var (
		configPath      string
		proxyCachesFlag string
		fastcgiFlag     string
		appName         string
		purgeCommand    string
	)

	flag.StringVar(&configPath, "config", "", "path to nginx config file")
	flag.StringVar(&proxyCachesFlag, "proxy-caches", "", "comma separated proxy cache names to purge")
	flag.StringVar(&fastcgiFlag, "fastcgi-caches", "", "comma separated fastcgi cache names to purge")
	flag.StringVar(&appName, "app-name", "", "app name used when rendering the nginx config (optional if cache paths are explicitly set)")
	flag.StringVar(&purgeCommand, "purge-command", "", "command used to perform cache purging")
	flag.Parse()

	if configPath == "" {
		log.Fatalln("missing required -config flag")
	}

	if appName == "" {
		// try common env fallbacks to reduce friction
		appName = os.Getenv("APP_NAME")
		if appName == "" {
			appName = os.Getenv("DOKKU_APP_NAME")
		}
	}

	proxyCaches := parseCSVFlag(proxyCachesFlag)
	fastcgiCaches := parseCSVFlag(fastcgiFlag)

	cfg, _, err := file_config.ReadConfig(configPath)
	if err != nil {
		log.Fatalln("error parsing config file:", err)
	}

	selectedProxy, err := ensureCacheNameExists(cfg.ProxyCaches, proxyCaches, "proxy")
	if err != nil {
		log.Fatalln(err)
	}
	selectedFastcgi, err := ensureCacheNameExists(cfg.FastcgiCaches, fastcgiCaches, "fastcgi")
	if err != nil {
		log.Fatalln(err)
	}

	proxyRoots := loadCacheRoots("proxy", len(selectedProxy) > 0)
	fastcgiRoots := loadCacheRoots("fastcgi", len(selectedFastcgi) > 0)

	for name, cacheCfg := range selectedProxy {
		cachePath, err := buildCachePath("proxy", appName, cacheCfg, proxyRoots)
		if err != nil {
			log.Fatalln(err)
		}
		log.Printf("Purging proxy cache %q at %s\n", name, cachePath)
		if err := purgeCacheDir(cachePath, purgeCommand); err != nil {
			log.Fatalln(err)
		}
	}

	for name, cacheCfg := range selectedFastcgi {
		cachePath, err := buildCachePath("fastcgi", appName, cacheCfg, fastcgiRoots)
		if err != nil {
			log.Fatalln(err)
		}
		log.Printf("Purging fastcgi cache %q at %s\n", name, cachePath)
		if err := purgeCacheDir(cachePath, purgeCommand); err != nil {
			log.Fatalln(err)
		}
	}
}
