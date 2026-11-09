package aigateway

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"

	"backend/internal/repo"

	"github.com/stretchr/testify/require"
)

func TestValidateBaseURL(t *testing.T) {
	tests := []struct {
		name          string
		raw           string
		trustedHosts  []string
		operatorHosts []string
		privateHosts  []string
		wantErr       error
	}{
		{
			name:         "allows trusted public HTTPS hostname",
			raw:          "https://api.openai.com/v1",
			trustedHosts: []string{"api.openai.com"},
		},
		{
			name:    "rejects public HTTPS hostname outside trust list",
			raw:     "https://api.deepseek.com/v1",
			wantErr: ErrUpstreamNotAllowed,
		},
		{
			name:          "operator ceiling can restrict trusted hostname",
			raw:           "https://api.deepseek.com/v1",
			trustedHosts:  []string{"api.deepseek.com"},
			operatorHosts: []string{"api.openai.com"},
			wantErr:       ErrUpstreamRestricted,
		},
		{
			name:         "allows HTTPS exact RFC1918 operator exception",
			raw:          "https://10.100.170.37:8443/v1",
			privateHosts: []string{"10.100.170.37"},
		},
		{
			name:    "rejects HTTPS private address without exception",
			raw:     "https://10.100.170.37:8443/v1",
			wantErr: ErrUpstreamRestricted,
		},
		{
			name:         "rejects HTTPS loopback even with exception",
			raw:          "https://127.0.0.1:8443/v1",
			privateHosts: []string{"127.0.0.1"},
			wantErr:      ErrInvalid,
		},
		{
			name:         "allows HTTP exact RFC1918 address",
			raw:          "http://10.100.170.37:8090/v1",
			privateHosts: []string{"10.100.170.37"},
		},
		{
			name:         "allows HTTP exact private 172 address",
			raw:          "http://172.20.251.55:8090/v1",
			privateHosts: []string{"172.20.251.55"},
		},
		{
			name:         "rejects HTTP private address not allowlisted",
			raw:          "http://10.100.170.37:8090/v1",
			privateHosts: []string{"10.100.170.38"},
			wantErr:      ErrUpstreamRestricted,
		},
		{
			name:         "rejects wildcard authorization for HTTP address",
			raw:          "http://10.100.170.37:8090/v1",
			privateHosts: []string{"*.100.170.37"},
			wantErr:      ErrUpstreamRestricted,
		},
		{
			name:    "rejects unlisted HTTP public address as invalid",
			raw:     "http://8.8.8.8/v1",
			wantErr: ErrInvalid,
		},
		{
			name:         "rejects HTTP public address even when allowlisted",
			raw:          "http://8.8.8.8/v1",
			privateHosts: []string{"8.8.8.8"},
			wantErr:      ErrInvalid,
		},
		{
			name:    "rejects unlisted HTTP hostname as invalid",
			raw:     "http://models.example.com/v1",
			wantErr: ErrInvalid,
		},
		{
			name:         "rejects HTTP hostname even when allowlisted",
			raw:          "http://models.example.com/v1",
			privateHosts: []string{"models.example.com"},
			wantErr:      ErrInvalid,
		},
		{
			name:         "rejects HTTP loopback even when allowlisted",
			raw:          "http://127.0.0.1:8090/v1",
			privateHosts: []string{"127.0.0.1"},
			wantErr:      ErrInvalid,
		},
		{
			name:         "rejects unsupported scheme",
			raw:          "ftp://10.100.170.37/v1",
			privateHosts: []string{"10.100.170.37"},
			wantErr:      ErrInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateBaseURLWithHosts(tt.raw, tt.trustedHosts, tt.operatorHosts, tt.privateHosts)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("validateBaseURL() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeSaveEmbedding(t *testing.T) {
	base := SaveCommand{
		Name:    "Enterprise AI",
		BaseURL: "https://api.openai.com/v1",
		APIKey:  "sk-test",
	}
	tests := []struct {
		name             string
		mutate           func(cmd *SaveCommand)
		creating         bool
		wantErr          error
		wantEmbeddingSet bool
		wantMode         string
	}{
		{
			name:             "no embedding fields keeps legacy semantics",
			mutate:           func(*SaveCommand) {},
			creating:         true,
			wantEmbeddingSet: false,
			wantMode:         EmbeddingModeInherit,
		},
		{
			name: "inherit mode allows empty embedding model",
			mutate: func(cmd *SaveCommand) {
				cmd.EmbeddingMode = EmbeddingModeInherit
			},
			creating:         true,
			wantEmbeddingSet: true,
			wantMode:         EmbeddingModeInherit,
		},
		{
			name: "model alone implies inherit mode",
			mutate: func(cmd *SaveCommand) {
				cmd.EmbeddingModel = "text-embedding-3-small"
			},
			creating:         true,
			wantEmbeddingSet: true,
			wantMode:         EmbeddingModeInherit,
		},
		{
			name: "custom mode requires base URL",
			mutate: func(cmd *SaveCommand) {
				cmd.EmbeddingMode = EmbeddingModeCustom
				cmd.EmbeddingModel = "text-embedding-3-small"
				cmd.EmbeddingAPIKey = "sk-embed"
			},
			creating: true,
			wantErr:  ErrInvalid,
		},
		{
			name: "custom mode requires key when creating",
			mutate: func(cmd *SaveCommand) {
				cmd.EmbeddingMode = EmbeddingModeCustom
				cmd.EmbeddingBaseURL = "https://api.openai.com/v1"
				cmd.EmbeddingModel = "text-embedding-3-small"
			},
			creating: true,
			wantErr:  ErrInvalid,
		},
		{
			name: "valid custom mode",
			mutate: func(cmd *SaveCommand) {
				cmd.EmbeddingMode = EmbeddingModeCustom
				cmd.EmbeddingBaseURL = "https://api.openai.com/v1"
				cmd.EmbeddingModel = "text-embedding-3-small"
				cmd.EmbeddingAPIKey = "sk-embed"
			},
			creating:         true,
			wantEmbeddingSet: true,
			wantMode:         EmbeddingModeCustom,
		},
		{
			name: "unknown mode rejected",
			mutate: func(cmd *SaveCommand) {
				cmd.EmbeddingMode = "auto"
				cmd.EmbeddingModel = "text-embedding-3-small"
			},
			creating: true,
			wantErr:  ErrInvalid,
		},
		{
			name: "custom embedding private base URL needs operator exception",
			mutate: func(cmd *SaveCommand) {
				cmd.EmbeddingMode = EmbeddingModeCustom
				cmd.EmbeddingBaseURL = "https://10.20.30.40/v1"
				cmd.EmbeddingModel = "text-embedding-3-small"
				cmd.EmbeddingAPIKey = "sk-embed"
			},
			creating: true,
			wantErr:  ErrUpstreamRestricted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &Service{}
			cmd := base
			tt.mutate(&cmd)
			item, _, _, embeddingSet, err := service.normalizeSave(context.Background(), cmd, tt.creating)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("normalizeSave() error = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if embeddingSet != tt.wantEmbeddingSet {
				t.Fatalf("embeddingSet = %v, want %v", embeddingSet, tt.wantEmbeddingSet)
			}
			if item.EmbeddingMode != tt.wantMode {
				t.Fatalf("EmbeddingMode = %q, want %q", item.EmbeddingMode, tt.wantMode)
			}
		})
	}
}

func TestOutboundIPAllowed(t *testing.T) {
	tests := []struct {
		name         string
		ip           string
		host         string
		allowedHosts []string
		want         bool
	}{
		{name: "public IPv4", ip: "8.8.8.8", host: "api.example.com", want: true},
		{name: "public IPv6", ip: "2606:4700:4700::1111", host: "api.example.com", want: true},
		{name: "private denied", ip: "10.1.2.3", host: "models.internal", want: false},
		{name: "private exact hostname exception", ip: "10.1.2.3", host: "models.internal", allowedHosts: []string{"models.internal"}, want: true},
		{name: "private wildcard hostname exception", ip: "fd00::10", host: "models.ai.internal", allowedHosts: []string{"*.ai.internal"}, want: true},
		{name: "loopback cannot be excepted", ip: "127.0.0.1", host: "127.0.0.1", allowedHosts: []string{"127.0.0.1"}, want: false},
		{name: "link local cannot be excepted", ip: "169.254.169.254", host: "metadata.internal", allowedHosts: []string{"metadata.internal"}, want: false},
		{name: "shared address space blocked", ip: "100.64.0.1", host: "api.example.com", want: false},
		{name: "documentation range blocked", ip: "203.0.113.10", host: "api.example.com", want: false},
		{name: "IPv4 IPv6 translation blocked", ip: "64:ff9b::a00:1", host: "api.example.com", want: false},
		{name: "6to4 transition blocked", ip: "2002:0a00:0001::", host: "api.example.com", want: false},
		{name: "deprecated IPv6 site local blocked", ip: "fec0::1", host: "api.example.com", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := outboundIPAllowed(net.ParseIP(tt.ip), tt.host, tt.allowedHosts); got != tt.want {
				t.Fatalf("outboundIPAllowed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeTrustedHosts(t *testing.T) {
	hosts, err := normalizeTrustedHosts([]string{" API.Example.com. ", "*.Models.Example.com", "api.example.com"})
	if err != nil {
		t.Fatalf("normalizeTrustedHosts() error = %v", err)
	}
	want := []string{"api.example.com", "*.models.example.com"}
	if len(hosts) != len(want) || hosts[0] != want[0] || hosts[1] != want[1] {
		t.Fatalf("normalizeTrustedHosts() = %#v, want %#v", hosts, want)
	}
	for _, invalid := range []string{"https://api.example.com", "api.example.com:443", "127.0.0.1", "localhost", "-api.example.com"} {
		if _, err := normalizeTrustedHosts([]string{invalid}); !errors.Is(err, ErrTrustedHostsInvalid) {
			t.Fatalf("normalizeTrustedHosts(%q) error = %v, want %v", invalid, err, ErrTrustedHostsInvalid)
		}
	}
}

func TestInitialTrustedHostsFromConfigs(t *testing.T) {
	hosts := initialTrustedHostsFromConfigs([]repo.AIProviderConfig{
		{BaseURL: "https://legacy.example.com/v1", EmbeddingBaseURL: "https://embeddings.example.net/v1"},
		{BaseURL: "https://API.OPENAI.COM/v1", EmbeddingBaseURL: "not a URL"},
		{BaseURL: "https://127.0.0.1/v1"},
	})
	require.Equal(t,
		append(append([]string(nil), defaultTrustedHosts...), "legacy.example.com", "embeddings.example.net"),
		hosts,
	)
}

func TestLegacyDefaultHostSet(t *testing.T) {
	if !isLegacyDefaultHostSet([]string{"api.anthropic.com", "*.openai.azure.com", "api.openai.com"}) {
		t.Fatal("legacy built-in host set should disable the operator ceiling")
	}
	if isLegacyDefaultHostSet([]string{"api.openai.com", "api.anthropic.com"}) {
		t.Fatal("customized operator host set must remain active")
	}
}

func TestInterleaveIPFamilies(t *testing.T) {
	addresses := []string{"2001:db8::1", "2001:db8::2", "192.0.2.1", "192.0.2.2"}
	parsed := make([]netip.Addr, 0, len(addresses))
	for _, raw := range addresses {
		parsed = append(parsed, netip.MustParseAddr(raw))
	}
	ordered := interleaveIPFamilies(parsed)
	want := []string{"2001:db8::1", "192.0.2.1", "2001:db8::2", "192.0.2.2"}
	for index, address := range ordered {
		if address.String() != want[index] {
			t.Fatalf("interleaveIPFamilies()[%d] = %s, want %s", index, address, want[index])
		}
	}
}
