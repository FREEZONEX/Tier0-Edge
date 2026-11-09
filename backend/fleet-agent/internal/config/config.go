package config

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"backend/internal/fleetprotocol"

	"github.com/google/uuid"
)

const (
	DefaultHTTPAddr              = "127.0.0.1:19731"
	DefaultHAServiceGroup        = "active"
	DefaultHAReconcileInterval   = 5 * time.Second
	DefaultHAHealthFailureWindow = 30 * time.Second
	DefaultHAOperationTimeout    = 2 * time.Minute
	DefaultHASyncInterval        = 10 * time.Second
	DefaultHAFullSyncInterval    = 2 * time.Minute
	DefaultHASyncTimeout         = 10 * time.Minute
	DefaultHARsyncSSHPort        = 22
	DefaultHARsyncMaxConcurrency = 2
)

type HAConfig struct {
	Enabled             bool
	MemberID            string
	VIP                 string
	Interface           string
	Priority            int
	ServiceGroup        string
	ReconcileInterval   time.Duration
	HealthFailureWindow time.Duration
	OperationTimeout    time.Duration
	Sync                HASyncConfig
}

type HASyncConfig struct {
	Enabled        bool
	Peers          []string
	SSHUser        string
	SSHPort        int
	SSHPrivateKey  string
	KnownHostsFile string
	FileSetsFile   string
	WorkDir        string
	Interval       time.Duration
	FullInterval   time.Duration
	Timeout        time.Duration
	MaxConcurrency int
}

type Config struct {
	UserspaceNetworkDir      string
	UserspaceSOCKSAddress    string
	NetworkMode              string
	BackendIngressPort       int
	SSHManifestFile          string
	SSHUserspaceForward      bool
	TokenNetworkBootstrapURL string
	NetworkBootstrapURL      string
	NetworkCAFile            string
	TailscaleBinary          string
	TailscaleSocket          string
	NetworkProfileHelper     bool
	WindowsManagedNetwork    bool
	NetworkOperator          string
	HTTPAddr                 string
	DataDir                  string
	CenterURL                string
	FleetNodeID              string
	MQTTAuthID               string
	MQTTBrokers              []string
	MQTTClientID             string
	MQTTUsername             string
	MQTTPassword             string
	AccessToken              string
	DesiredConnected         bool
	LocalTokenHash           []byte
	InstallationID           string
	ComposeProjectName       string
	HA                       HAConfig
}

type Persisted struct {
	CenterURL          string
	FleetNodeID        string
	AccessToken        string
	DesiredConnected   bool
	LocalTokenHash     []byte
	InstallationID     string
	ComposeProjectName string
	LastConnectedAt    int64
}

type PersistentLoader interface {
	LoadConfig(context.Context) (Persisted, bool, error)
}

func LoadFromEnv() (Config, error) {
	return Load(context.Background(), nil)
}

func Load(ctx context.Context, loader PersistentLoader) (Config, error) {
	if err := validateTypedEnvironment(); err != nil {
		return Config{}, err
	}
	dataDir, err := ResolveDataDir("")
	if err != nil {
		return Config{}, err
	}
	persisted := Persisted{}
	persistedFound := false
	if loader != nil {
		persisted, persistedFound, err = loader.LoadConfig(ctx)
		if err != nil {
			return Config{}, fmt.Errorf("load persistent configuration: %w", err)
		}
		if !persistedFound {
			persisted = Persisted{}
		}
	}
	desiredConnected := true
	if persistedFound {
		desiredConnected = persisted.DesiredConnected
	}

	cfg := Config{
		UserspaceNetworkDir:   strings.TrimSpace(os.Getenv("FLEET_USERSPACE_NETWORK_DIR")),
		UserspaceSOCKSAddress: strings.TrimSpace(os.Getenv("FLEET_NETWORK_SOCKS_ADDRESS")),
		SSHManifestFile:       strings.TrimSpace(os.Getenv("FLEET_SSH_MANIFEST_FILE")),
		SSHUserspaceForward:   envBool("FLEET_SSH_USERSPACE_FORWARD", false),
		BackendIngressPort:    envInt("FLEET_BACKEND_INGRESS_PORT", 0),
		NetworkBootstrapURL:   strings.TrimSpace(os.Getenv("FLEET_NETWORK_CENTER_URL")),
		NetworkCAFile:         strings.TrimSpace(os.Getenv("FLEET_NETWORK_CA_FILE")),
		TailscaleBinary:       strings.TrimSpace(os.Getenv("FLEET_TAILSCALE_BINARY")),
		TailscaleSocket:       strings.TrimSpace(os.Getenv("FLEET_TAILSCALE_SOCKET")),
		NetworkProfileHelper:  envBool("FLEET_NETWORK_PROFILE_HELPER", false),
		WindowsManagedNetwork: envBool("FLEET_WINDOWS_MANAGED_NETWORK", false),
		NetworkOperator:       strings.TrimSpace(os.Getenv("FLEET_AGENT_RUN_USER")),
		HTTPAddr:              envOrDefault("FLEET_AGENT_HTTP_ADDR", DefaultHTTPAddr),
		DataDir:               dataDir,
		AccessToken:           strings.TrimSpace(persisted.AccessToken),
		DesiredConnected:      desiredConnected,
		LocalTokenHash:        append([]byte(nil), persisted.LocalTokenHash...),
		InstallationID:        firstNonEmptyEnv("TIER0_INSTALLATION_ID", persisted.InstallationID),
		ComposeProjectName:    firstNonEmptyEnv("COMPOSE_PROJECT_NAME", persisted.ComposeProjectName),
		HA: HAConfig{
			Enabled:             envBool("FLEET_HA_ENABLED", false),
			MemberID:            strings.TrimSpace(os.Getenv("FLEET_HA_MEMBER_ID")),
			VIP:                 strings.TrimSpace(os.Getenv("FLEET_HA_VIP")),
			Interface:           strings.TrimSpace(os.Getenv("FLEET_HA_INTERFACE")),
			Priority:            envInt("FLEET_HA_PRIORITY", 100),
			ServiceGroup:        envOrDefault("FLEET_HA_SERVICE_GROUP", DefaultHAServiceGroup),
			ReconcileInterval:   envDuration("FLEET_HA_RECONCILE_INTERVAL", DefaultHAReconcileInterval),
			HealthFailureWindow: envDuration("FLEET_HA_HEALTH_FAILURE_WINDOW", DefaultHAHealthFailureWindow),
			OperationTimeout:    envDuration("FLEET_HA_OPERATION_TIMEOUT", DefaultHAOperationTimeout),
			Sync: HASyncConfig{
				Enabled:        envBool("FLEET_HA_SYNC_ENABLED", false),
				Peers:          envListWithFallback("FLEET_HA_RSYNC_PEERS", "FLEET_HA_UNICAST_PEERS"),
				SSHUser:        strings.TrimSpace(os.Getenv("FLEET_HA_RSYNC_USER")),
				SSHPort:        envInt("FLEET_HA_RSYNC_SSH_PORT", DefaultHARsyncSSHPort),
				SSHPrivateKey:  strings.TrimSpace(os.Getenv("FLEET_HA_RSYNC_SSH_KEY")),
				KnownHostsFile: strings.TrimSpace(os.Getenv("FLEET_HA_RSYNC_KNOWN_HOSTS_FILE")),
				FileSetsFile:   strings.TrimSpace(os.Getenv("FLEET_HA_FILESETS_FILE")),
				WorkDir:        strings.TrimSpace(os.Getenv("FLEET_HA_SYNC_WORK_DIR")),
				Interval:       envDuration("FLEET_HA_SYNC_INTERVAL", DefaultHASyncInterval),
				FullInterval:   envDuration("FLEET_HA_FULL_SYNC_INTERVAL", DefaultHAFullSyncInterval),
				Timeout:        envDuration("FLEET_HA_SYNC_TIMEOUT", DefaultHASyncTimeout),
				MaxConcurrency: envInt("FLEET_HA_RSYNC_MAX_CONCURRENCY", DefaultHARsyncMaxConcurrency),
			},
		},
	}
	if accessToken := strings.TrimSpace(os.Getenv("FLEET_ACCESS_TOKEN")); accessToken != "" {
		cfg.AccessToken = accessToken
		if strings.TrimSpace(persisted.AccessToken) == "" {
			cfg.DesiredConnected = true
		}
	}
	if localToken := strings.TrimSpace(os.Getenv("FLEET_AGENT_LOCAL_TOKEN")); localToken != "" {
		cfg.LocalTokenHash = HashLocalToken(localToken)
	}

	if err := ApplyConnectionToken(&cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.UserspaceNetworkDir != "" {
		if !filepath.IsAbs(c.UserspaceNetworkDir) || c.NetworkProfileHelper || c.WindowsManagedNetwork || c.TailscaleSocket != "" || c.TailscaleBinary != "" {
			return errors.New("isolated userspace networking requires an absolute private directory and no host client configuration")
		}
		host, _, err := net.SplitHostPort(c.UserspaceSOCKSAddress)
		ip := net.ParseIP(host)
		if err != nil || ip == nil || !ip.IsLoopback() {
			return errors.New("FLEET_NETWORK_SOCKS_ADDRESS_INVALID")
		}
	}
	if _, _, err := net.SplitHostPort(c.HTTPAddr); err != nil {
		return fmt.Errorf("invalid FLEET_AGENT_HTTP_ADDR: %w", err)
	}

	if c.AccessToken == "" {
		if c.FleetNodeID != "" || c.MQTTAuthID != "" || len(c.MQTTBrokers) != 0 || c.MQTTClientID != "" || c.MQTTUsername != "" || c.MQTTPassword != "" {
			return errors.New("Fleet connection identity requires FLEET_ACCESS_TOKEN")
		}
	} else if c.FleetNodeID == "" || c.MQTTAuthID == "" || len(c.MQTTBrokers) == 0 || c.MQTTClientID == "" || c.MQTTUsername == "" || c.MQTTPassword == "" {
		return errors.New("FLEET_ACCESS_TOKEN does not contain a complete Fleet connection")
	}

	if strings.TrimSpace(c.DataDir) == "" {
		return errors.New("FLEET_AGENT_DATA_DIR must not be empty")
	}
	if len(c.LocalTokenHash) != 0 && len(c.LocalTokenHash) != sha256.Size {
		return errors.New("local API token hash is invalid")
	}
	if c.InstallationID != "" {
		if len(c.InstallationID) < 8 || len(c.InstallationID) > 128 {
			return errors.New("TIER0_INSTALLATION_ID must contain 8-128 characters")
		}
		for _, character := range c.InstallationID {
			if !isInstallationIDCharacter(character) {
				return errors.New("TIER0_INSTALLATION_ID may contain only letters, digits, '.', '_', and '-'")
			}
		}
	}
	if err := c.HA.validate(c); err != nil {
		return err
	}
	return nil
}

func (c HAConfig) validate(parent Config) error {
	if !c.Enabled {
		return nil
	}
	if runtime.GOOS != "linux" {
		return errors.New("Fleet HA is supported only on Linux")
	}
	if strings.TrimSpace(parent.InstallationID) == "" {
		return errors.New("FLEET_HA_ENABLED requires TIER0_INSTALLATION_ID")
	}
	if len(parent.LocalTokenHash) != sha256.Size {
		return errors.New("FLEET_HA_ENABLED requires FLEET_AGENT_LOCAL_TOKEN")
	}
	if strings.TrimSpace(c.VIP) == "" {
		return errors.New("FLEET_HA_ENABLED requires FLEET_HA_VIP")
	}
	if net.ParseIP(c.VIP) == nil {
		return errors.New("FLEET_HA_VIP must be an IP address")
	}
	if strings.TrimSpace(c.Interface) == "" {
		return errors.New("FLEET_HA_ENABLED requires FLEET_HA_INTERFACE")
	}
	if strings.TrimSpace(c.ServiceGroup) == "" {
		return errors.New("FLEET_HA_SERVICE_GROUP must not be empty")
	}
	if c.Priority < 1 || c.Priority > 254 {
		return errors.New("FLEET_HA_PRIORITY must be between 1 and 254")
	}
	if c.MemberID != "" {
		if _, err := uuid.Parse(c.MemberID); err != nil {
			return errors.New("FLEET_HA_MEMBER_ID must be a UUID")
		}
	}
	if c.ReconcileInterval <= 0 || c.HealthFailureWindow <= 0 || c.OperationTimeout <= 0 {
		return errors.New("Fleet HA health and operation timeouts must be positive")
	}
	return c.Sync.validate()
}

func (c HASyncConfig) validate() error {
	if !c.Enabled {
		return nil
	}
	if len(c.Peers) == 0 {
		return errors.New("Fleet HA rsync requires FLEET_HA_RSYNC_PEERS or FLEET_HA_UNICAST_PEERS")
	}
	for _, peer := range c.Peers {
		if net.ParseIP(peer) == nil {
			return fmt.Errorf("Fleet HA rsync peer must be an IP address: %s", peer)
		}
	}
	if strings.TrimSpace(c.SSHUser) == "" || strings.TrimSpace(c.SSHPrivateKey) == "" ||
		strings.TrimSpace(c.KnownHostsFile) == "" {
		return errors.New("Fleet HA rsync requires SSH user, private key, and known_hosts file")
	}
	if !safeSSHUser(c.SSHUser) {
		return errors.New("FLEET_HA_RSYNC_USER contains unsupported characters")
	}
	if !safeCommandPath(c.SSHPrivateKey) || !safeCommandPath(c.KnownHostsFile) {
		return errors.New("Fleet HA rsync SSH key and known_hosts paths must be safe absolute paths")
	}
	if c.SSHPort < 1 || c.SSHPort > 65535 {
		return errors.New("FLEET_HA_RSYNC_SSH_PORT must be between 1 and 65535")
	}
	if strings.TrimSpace(c.FileSetsFile) == "" {
		return errors.New("Fleet HA rsync requires FLEET_HA_FILESETS_FILE")
	}
	if c.Interval <= 0 || c.FullInterval <= 0 || c.Timeout <= 0 {
		return errors.New("Fleet HA rsync intervals and timeout must be positive")
	}
	if c.MaxConcurrency < 1 || c.MaxConcurrency > 16 {
		return errors.New("FLEET_HA_RSYNC_MAX_CONCURRENCY must be between 1 and 16")
	}
	return nil
}

func (c Config) CenterConfigured() bool {
	return c.AccessToken != "" && c.FleetNodeID != "" && c.MQTTAuthID != "" && len(c.MQTTBrokers) > 0
}

func ApplyConnectionToken(cfg *Config) error {
	if cfg == nil {
		return errors.New("Fleet Agent configuration is nil")
	}
	cfg.NetworkMode = ""
	cfg.CenterURL = ""
	cfg.TokenNetworkBootstrapURL = ""
	cfg.FleetNodeID = ""
	cfg.MQTTAuthID = ""
	cfg.MQTTBrokers = nil
	cfg.MQTTClientID = ""
	cfg.MQTTUsername = ""
	cfg.MQTTPassword = ""
	if strings.TrimSpace(cfg.AccessToken) == "" {
		cfg.AccessToken = ""
		return nil
	}
	parsed, err := fleetprotocol.ParseConnectionToken(cfg.AccessToken)
	if err != nil {
		return fmt.Errorf("parse FLEET_ACCESS_TOKEN: %w", err)
	}
	cfg.NetworkMode = parsed.NetworkMode
	cfg.CenterURL = parsed.HTTPBaseURL
	cfg.TokenNetworkBootstrapURL = parsed.NetworkBootstrapURL
	cfg.FleetNodeID = parsed.NodeID
	cfg.MQTTAuthID = parsed.AuthID
	cfg.MQTTBrokers = parsed.BrokerURLs()
	cfg.MQTTClientID = parsed.AgentClientID()
	cfg.MQTTUsername = parsed.Username
	cfg.MQTTPassword = parsed.Password
	return nil
}

func ResolveDataDir(override string) (string, error) {
	if dataDir := strings.TrimSpace(override); dataDir != "" {
		return filepath.Clean(dataDir), nil
	}
	if dataDir := strings.TrimSpace(os.Getenv("FLEET_AGENT_DATA_DIR")); dataDir != "" {
		return filepath.Clean(dataDir), nil
	}
	baseDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(baseDir, "Tier0", "FleetAgent"), nil
}

func HashLocalToken(token string) []byte {
	digest := sha256.Sum256([]byte(token))
	return append([]byte(nil), digest[:]...)
}

func firstNonEmptyEnv(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

func isInstallationIDCharacter(character rune) bool {
	return character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' ||
		character == '.' || character == '_' || character == '-'
}

func envOrDefault(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func validateTypedEnvironment() error {
	for _, name := range []string{
		"FLEET_WINDOWS_MANAGED_NETWORK",
		"FLEET_SSH_USERSPACE_FORWARD",
		"FLEET_HA_ENABLED",
		"FLEET_HA_SYNC_ENABLED",
	} {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			continue
		}
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("%s must be a boolean: %w", name, err)
		}
	}
	for _, name := range []string{
		"FLEET_HA_RECONCILE_INTERVAL",
		"FLEET_HA_HEALTH_FAILURE_WINDOW",
		"FLEET_HA_OPERATION_TIMEOUT",
		"FLEET_HA_SYNC_INTERVAL",
		"FLEET_HA_FULL_SYNC_INTERVAL",
		"FLEET_HA_SYNC_TIMEOUT",
	} {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			continue
		}
		if _, err := time.ParseDuration(value); err != nil {
			return fmt.Errorf("%s must be a Go duration: %w", name, err)
		}
	}
	if value := strings.TrimSpace(os.Getenv("FLEET_HA_PRIORITY")); value != "" {
		if _, err := strconv.Atoi(value); err != nil {
			return fmt.Errorf("FLEET_HA_PRIORITY must be an integer: %w", err)
		}
	}
	for _, name := range []string{"FLEET_HA_RSYNC_SSH_PORT", "FLEET_HA_RSYNC_MAX_CONCURRENCY"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			if _, err := strconv.Atoi(value); err != nil {
				return fmt.Errorf("%s must be an integer: %w", name, err)
			}
		}
	}
	return nil
}

func envListWithFallback(name, fallbackName string) []string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		value = strings.TrimSpace(os.Getenv(fallbackName))
	}
	if value == "" {
		return nil
	}
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func safeSSHUser(value string) bool {
	for index, character := range strings.TrimSpace(value) {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character == '_' ||
			index > 0 && character >= '0' && character <= '9' ||
			index > 0 && (character == '.' || character == '-') {
			continue
		}
		return false
	}
	return strings.TrimSpace(value) != ""
}

func safeCommandPath(value string) bool {
	value = filepath.ToSlash(strings.TrimSpace(value))
	if !strings.HasPrefix(value, "/") {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '/' || character == '.' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}
