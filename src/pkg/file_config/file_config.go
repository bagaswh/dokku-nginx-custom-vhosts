// Retrieves config from yaml config set in nginx-custom-vhost-config-file Dokku config key

package file_config

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/jmespath/go-jmespath"
	"gopkg.in/yaml.v3"
)

type UpstreamServer struct {
	Addr  string            `yaml:"addr" validate:"required" json:"addr"`
	Flags map[string]string `yaml:"flags" validate:"required" json:"flags"`
}

type UpstreamServerFlags struct {
	Selector string            `yaml:"selector" validate:"required" json:"selector"`
	Flags    map[string]string `yaml:"flags" validate:"required" json:"flags"`
}

type UpstreamConfig struct {
	// Select and Name are mutually exclusive
	// SelectProcessType   string                `yaml:"select_process_type" validate:"excluded_with=Name,excluded_with=Servers" json:"select_process_type"`
	// DefaultServersFlags []UpstreamServerFlags `yaml:"default_servers_flags" validate:"required_with=SelectFromProcessType" json:"default_servers_flags"`

	Name    string           `yaml:"name" validate:"required_if=SelectProcessType ''" json:"name"`
	Servers []UpstreamServer `yaml:"servers" validate:"required_if=Name true,excluded_with=select_process_type" json:"servers"`
}

type LocationConfig struct {
	Modifier string `yaml:"modifier" validate:"omitempty,excluded_without=Uri" json:"modifier"`
	Uri      string `yaml:"uri" validate:"excluded_with=Named" json:"uri"`
	Named    string `yaml:"named" validate:"excluded_with=Uri,excluded_with=Modifier" json:"named"`
	Body     string `yaml:"body" validate:"required" json:"body"`
}

type MapConfig struct {
	Variable string `yaml:"variable" validate:"required" json:"variable"`
	String   string `yaml:"string" validate:"required" json:"string"`
	Lines    string `yaml:"lines" validate:"required" json:"lines"`
}

type VariableConfig struct {
	Name  string `yaml:"name" validate:"required" json:"name"`
	Value string `yaml:"value" validate:"required" json:"value"`
}

type CacheConfig struct {
	Name          string            `yaml:"name" validate:"required" json:"name"`
	CachePath     string            `yaml:"proxy_cache_path" json:"proxy_cache_path"`
	KeyZoneSize   string            `yaml:"key_zone_size" json:"key_zone_size"`
	Flags         map[string]string `json:"flags" yaml:"flags"`
	InMem         bool              `yaml:"in_mem" json:"in_mem" validate:"excluded_if=OnDisk true"`
	OnDisk        bool              `yaml:"on_disk" json:"on_disk" validate:"excluded_if=InMem true"`
	PurgeOnDeploy bool              `yaml:"purge_on_deploy" json:"purge_on_deploy"`
}

type VhostConfig struct {
	ServerName string           `yaml:"server_name" validate:"required" json:"server_name"`
	Locations  []LocationConfig `yaml:"locations" validate:"required,dive" json:"locations"`
	Variables  []VariableConfig `yaml:"variables" validate:"omitempty,dive" json:"variables"`

	InServerBlock string `yaml:"in_server_block" validate:"omitempty" json:"in_server_block"`
}

type ConfigVars map[string]any

type Config struct {
	Vhosts []VhostConfig `yaml:"vhosts" validate:"required,dive"`

	SysVars  ConfigVars
	UserVars ConfigVars `yaml:"user_vars" validate:"omitempty" json:"vars"`

	UpstreamAddressmode string           `yaml:"upstream_address_mode" validate:"oneof=ip dns" json:"upstream_address_mode"`
	Upstreams           []UpstreamConfig `yaml:"upstreams" validate:"omitempty,dive" json:"upstreams"`
	Maps                []MapConfig      `yaml:"maps" validate:"omitempty,dive" json:"maps"`
	ProxyCaches         []CacheConfig    `yaml:"proxy_caches" validate:"omitempty,dive" json:"proxy_caches"`
	FastcgiCaches       []CacheConfig    `yaml:"fastcgi_caches" validate:"omitempty,dive" json:"fastcgi_caches"`

	InHttpBlock string `yaml:"in_http_block" validate:"omitempty" json:"in_http_block"`
}

func registerValidations(validate *validator.Validate) {
	// validate.RegisterValidation("excluded_with", func(fl validator.FieldLevel) bool {
	// 	field := fl.Field()
	// 	if field.IsZero() {
	// 		return true
	// 	}

	// 	other := fl.Parent().FieldByName(fl.Param())
	// 	return other.IsZero()
	// })
}

func validateConfig(config *Config) error {
	validate := validator.New()
	registerValidations(validate)

	// Register custom error messages
	validate.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return fld.Name
		}
		return name
	})

	err := validate.Struct(config)
	if err != nil {
		if _, ok := err.(*validator.InvalidValidationError); ok {
			return fmt.Errorf("internal validation error: %v", err)
		}

		var errorMessages []string
		for _, err := range err.(validator.ValidationErrors) {
			// Get the full namespace and format it for readability
			namespace := err.Namespace()

			// Split the namespace into parts
			parts := strings.Split(strings.TrimPrefix(namespace, "Config."), ".")
			var pathParts []string

			for i, part := range parts {
				// Handle array indices
				if strings.Contains(part, "[") {
					base := part[:strings.Index(part, "[")]
					index := part[strings.Index(part, "[")+1 : strings.Index(part, "]")]

					// Make the path more readable based on the parent type
					pathParts = append(pathParts, fmt.Sprintf("%s #%s", strings.ToLower(base), index))
				} else if i > 0 {
					// Only add if it's not an array field that will be handled by its parent
					if !strings.HasSuffix(parts[i-1], "]") {
						pathParts = append(pathParts, strings.ToLower(part))
					}
				}
			}

			path := strings.Join(pathParts, " ")

			// Format the error message based on the validation tag
			var msg string
			switch err.Tag() {
			case "required":
				msg = fmt.Sprintf("field '%s' is required", err.Field())
			case "required_without":
				msg = fmt.Sprintf("field '%s' is required when '%s' is not provided", err.Field(), err.Param())
			case "excluded_with":
				msg = fmt.Sprintf("field '%s' cannot be used together with '%s'", err.Field(), err.Param())
			case "min":
				msg = fmt.Sprintf("field '%s' must have at least %s items", err.Field(), err.Param())
			case "required_if":
				msg = fmt.Sprintf("field '%s' is required when %s", err.Field(), err.Param())
			default:
				msg = fmt.Sprintf("field '%s' failed validation: %s", err.Field(), err.Tag())
			}

			errorMessages = append(errorMessages, fmt.Sprintf("In %s: %s", path, msg))
		}
		return fmt.Errorf("validation errors:\n- %s", strings.Join(errorMessages, "\n- "))
	}
	return nil
}

func ReadConfig(path string) (*Config, any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, nil, err
	}

	// Set default value if not provided
	if config.UpstreamAddressmode == "" {
		config.UpstreamAddressmode = "ip"
	}

	// Validate config
	if err := validateConfig(&config); err != nil {
		return nil, nil, fmt.Errorf("config validation failed: %v", err)
	}

	var rawConfig interface{}
	if err := yaml.Unmarshal(data, &rawConfig); err != nil {
		return nil, nil, fmt.Errorf("error parsing YAML into config struct: %v", err)
	}

	return &config, rawConfig, nil
}

var ErrWalkSkip = errors.New("walk skipped")

func QueryConfig(data interface{}, query string) (interface{}, error) {
	return jmespath.Search(query, data)
}
