// Package configuration is in charge of the validation and extraction of all
// the configuration details from a configuration file or environment variables.
package configuration

import (
	"flag"
	"os"
	"strings"
	"time"

	errs "github.com/pkg/errors"
	"github.com/spf13/viper"
	"sigs.k8s.io/kubefed/pkg/apis/core/v1beta1/defaults"
)

// prefixes
const (
	// MemberEnvPrefix will be used for member environment variable name prefixing.
	MemberEnvPrefix = "MEMBER_OPERATOR"
)

// Configuration constants
const (
	// ToolchainConfigMapName specifies a name of a ConfigMap that keeps toolchain configuration
	ToolchainConfigMapName = "toolchain-saas-config"

	// IdentityProvider specifies an identity provider (IdP) for newly created users
	IdentityProvider = "identity.provider"

	// DefaultIdentityProvider the default value used for the identity provider (IdP) for newly created users
	DefaultIdentityProvider = "rhd"
)

// Kubefed configuration constants
const (
	ClusterHealthCheckPeriod        = "cluster.healthcheck.period"
	DefaultClusterHealthCheckPeriod = defaults.DefaultClusterHealthCheckPeriod

	ClusterHealthCheckTimeout        = "cluster.healthcheck.timeout"
	DefaultClusterHealthCheckTimeout = defaults.DefaultClusterHealthCheckTimeout

	ClusterHealthCheckFailureThreshold        = "cluster.healthcheck.failurethreshold"
	DefaultClusterHealthCheckFailureThreshold = defaults.DefaultClusterHealthCheckFailureThreshold

	ClusterHealthCheckSuccessThreshold        = "cluster.healthcheck.successthreshold"
	DefaultClusterHealthCheckSuccessThreshold = defaults.DefaultClusterHealthCheckSuccessThreshold

	ClusterAvailableDelay        = "cluster.available.delay"
	DefaultClusterAvailableDelay = defaults.DefaultClusterAvailableDelay

	ClusterUnavailableDelay        = "cluster.unavailable.delay"
	DefaultClusterUnavailableDelay = defaults.DefaultClusterUnavailableDelay
)

// Registry encapsulates the Viper configuration registry which stores the
// configuration data in-memory.
type Registry struct {
	member *viper.Viper
}

// createEmptyRegistry creates an initial, empty registry.
func createEmptyRegistry() *Registry {
	c := Registry{
		member: viper.New(),
	}
	c.member.SetEnvPrefix(MemberEnvPrefix)
	c.member.AutomaticEnv()
	c.member.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	c.setConfigDefaults()
	return &c
}

func LoadConfig() (*Registry, error) {
	var configFilePath string
	flag.StringVar(&configFilePath, "config", "", "path to the config file to read (if none is given, defaults will be used)")

	// Override default -config switch with environment variable only if -config
	// switch was not explicitly given via the command line.
	configSwitchIsSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			configSwitchIsSet = true
		}
	})
	if !configSwitchIsSet {
		if envConfigPath, ok := os.LookupEnv(MemberEnvPrefix + "_CONFIG_FILE_PATH"); ok {
			configFilePath = envConfigPath
		}
	}

	return New(configFilePath)
}

// New creates a configuration reader object using a configurable configuration
// file path. If the provided config file path is empty, a default configuration
// will be created.
func New(configFilePath string) (*Registry, error) {
	c := createEmptyRegistry()
	if configFilePath != "" {
		c.member.SetConfigType("yaml")
		c.member.SetConfigFile(configFilePath)
		err := c.member.ReadInConfig() // Find and read the config file
		if err != nil {                // Handle errors reading the config file.
			return nil, errs.Wrap(err, "failed to read config file")
		}
	}
	return c, nil
}

func (c *Registry) setConfigDefaults() {
	c.member.SetTypeByDefaultValue(true)
	c.member.SetDefault(IdentityProvider, DefaultIdentityProvider)
	c.member.SetDefault(ClusterHealthCheckPeriod, DefaultClusterHealthCheckPeriod)
	c.member.SetDefault(ClusterHealthCheckTimeout, DefaultClusterHealthCheckTimeout)
	c.member.SetDefault(ClusterHealthCheckFailureThreshold, DefaultClusterHealthCheckFailureThreshold)
	c.member.SetDefault(ClusterHealthCheckSuccessThreshold, DefaultClusterHealthCheckSuccessThreshold)
}

// GetAllMemberParameters returns the map with key-values pairs of parameters that have MEMBER_OPERATOR prefix
func (c *Registry) GetAllMemberParameters() map[string]string {
	vars := map[string]string{}

	for _, env := range os.Environ() {
		keyValue := strings.SplitN(env, "=", 2)
		if len(keyValue) < 2 {
			continue
		}
		if strings.HasPrefix(keyValue[0], MemberEnvPrefix+"_") {
			vars[keyValue[0]] = keyValue[1]
		}
	}
	return vars
}

// GetIdP returns the configured Identity Provider (IdP) for the member operator
// Openshift clusters can be configured with multiple IdPs. This config option allows admins to specify which IdP should be used by the toolchain operator.
func (c *Registry) GetIdP() string {
	return c.member.GetString(IdentityProvider)
}

// GetClusterHealthCheckPeriod returns the configured member cluster health check period
func (c *Registry) GetClusterHealthCheckPeriod() time.Duration {
	return c.member.GetDuration(ClusterHealthCheckPeriod)
}

// GetClusterHealthCheckTimeout returns the configured member cluster health check timeout
func (c *Registry) GetClusterHealthCheckTimeout() time.Duration {
	return c.member.GetDuration(ClusterHealthCheckTimeout)
}

// GetClusterHealthCheckFailureThreshold returns the configured member cluster health check failure threshold
func (c *Registry) GetClusterHealthCheckFailureThreshold() int64 {
	return c.member.GetInt64(ClusterHealthCheckFailureThreshold)
}

// GetClusterHealthCheckSuccessThreshold returns the configured member cluster health check failure threshold
func (c *Registry) GetClusterHealthCheckSuccessThreshold() int64 {
	return c.member.GetInt64(ClusterHealthCheckSuccessThreshold)
}

// GetClusterAvailableDelay returns the configured member cluster available delay
func (c *Registry) GetClusterAvailableDelay() time.Duration {
	return c.member.GetDuration(ClusterAvailableDelay)
}

// GetClusterUnavailableDelay returns the configured member cluster unavailable delay
func (c *Registry) GetClusterUnavailableDelay() time.Duration {
	return c.member.GetDuration(ClusterUnavailableDelay)
}