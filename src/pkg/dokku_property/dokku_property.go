package dokkuproperty

import (
	"os"

	"github.com/dokku/dokku/plugins/common"
)

// Add these structs at the top of the file
type PropertyConfig struct {
	Name         string
	DefaultValue string
	UsesAppName  bool
}

// Generic property getters
func GetAppProperty(appName string, property string) string {
	return common.PropertyGet(getProxyName(), appName, property)
}

func GetComputedProperty(appName string, property string) string {
	appValue := GetAppProperty(appName, property)
	if appValue != "" {
		return appValue
	}

	return GetGlobalProperty(appName, property)
}

func GetGlobalProperty(appName string, property string) string {
	return common.PropertyGet(getProxyName(), "--global", property)
}

func getProxyName() string {
	if v := os.Getenv("PROXY_NAME"); v != "" {
		return v
	}
	return "nginx-custom"
}
