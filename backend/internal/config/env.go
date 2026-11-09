package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const clusterNodeKeyFile = ".cluster-node-key"

const DefaultTimeseriesRetentionYears = 10
const DefaultSampleCompressionAfterDays = 90

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func expandEnv(value string) string {
	return os.ExpandEnv(value)
}

func getenvBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func getenvInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func NormalizeTimeseriesRetentionYears(configured int) int {
	if configured <= 0 {
		configured = DefaultTimeseriesRetentionYears
	}
	years := getenvInt("TIMESERIES_RETENTION_YEARS", configured)
	if years <= 0 {
		return DefaultTimeseriesRetentionYears
	}
	return years
}

// Zero pauses automatic compression without decompressing retained history.
func SampleCompressionAfterDays() int {
	days := getenvInt("SAMPLE_COMPRESSION_AFTER_DAYS", DefaultSampleCompressionAfterDays)
	if days < 0 {
		return DefaultSampleCompressionAfterDays
	}
	return days
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func defaultNodeKey(stateDir string) string {
	if key := readPersistedNodeKey(stateDir); key != "" {
		return key
	}
	if key := newRandomNodeKey(); key != "" && writePersistedNodeKey(stateDir, key) {
		return key
	}
	return fallbackNodeKey()
}

func readPersistedNodeKey(stateDir string) string {
	if strings.TrimSpace(stateDir) == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(stateDir, clusterNodeKeyFile))
	if err != nil {
		return ""
	}
	return sanitizeNodeKey(string(data))
}

func writePersistedNodeKey(stateDir, key string) bool {
	if strings.TrimSpace(stateDir) == "" || strings.TrimSpace(key) == "" {
		return false
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return false
	}
	return os.WriteFile(filepath.Join(stateDir, clusterNodeKeyFile), []byte(key+"\n"), 0o600) == nil
}

func newRandomNodeKey() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return ""
	}
	return "edge-" + hex.EncodeToString(raw[:])
}

func fallbackNodeKey() string {
	name, err := os.Hostname()
	if err != nil {
		return "edge-local"
	}
	name = sanitizeNodeKey(name)
	if name == "" {
		return "edge-local"
	}
	return name
}

func sanitizeNodeKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(value, "-")
	return strings.Trim(value, "-")
}
