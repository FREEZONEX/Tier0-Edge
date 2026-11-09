package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalize_DataQualityMaxCollectWindow(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv("DATA_QUALITY_MAX_COLLECT_WINDOW_MS", "")
		cfg := Config{}
		cfg.Normalize()
		require.Equal(t, 600000, cfg.DataQuality.MaxCollectWindowMs)
	})

	t.Run("environment override", func(t *testing.T) {
		t.Setenv("DATA_QUALITY_MAX_COLLECT_WINDOW_MS", "120000")
		cfg := Config{DataQuality: DataQualityConf{MaxCollectWindowMs: 600000}}
		cfg.Normalize()
		require.Equal(t, 120000, cfg.DataQuality.MaxCollectWindowMs)
	})
}
