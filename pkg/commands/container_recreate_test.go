package commands

import (
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

func TestFilterAliases(t *testing.T) {
	type scenario struct {
		name     string
		aliases  []string
		shortID  string
		hostname string
		expected []string
	}

	scenarios := []scenario{
		{
			name:     "removes short id and hostname",
			aliases:  []string{"web", "abc123456789", "myhost"},
			shortID:  "abc123456789",
			hostname: "myhost",
			expected: []string{"web"},
		},
		{
			name:     "keeps user aliases",
			aliases:  []string{"web", "api"},
			shortID:  "abc123456789",
			hostname: "myhost",
			expected: []string{"web", "api"},
		},
		{
			name:     "returns nil when all filtered",
			aliases:  []string{"abc123456789", "myhost"},
			shortID:  "abc123456789",
			hostname: "myhost",
			expected: nil,
		},
		{
			name:     "handles empty input",
			aliases:  nil,
			shortID:  "abc",
			hostname: "host",
			expected: nil,
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			got := filterAliases(s.aliases, s.shortID, s.hostname)
			if len(got) != len(s.expected) {
				t.Fatalf("filterAliases() = %v, want %v", got, s.expected)
			}
			for i := range got {
				if got[i] != s.expected[i] {
					t.Fatalf("filterAliases() = %v, want %v", got, s.expected)
				}
			}
		})
	}
}

func TestBuildNetworkingConfig(t *testing.T) {
	details := container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{
			ID: "abcdef1234567890",
		},
		Config: &container.Config{
			Hostname: "myhost",
		},
		NetworkSettings: &container.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"bridge": {
					NetworkID: "net123",
					Aliases:   []string{"web", "abcdef123456", "myhost"},
					// Runtime-only fields that should not be carried over.
					IPAddress: "172.17.0.2",
					Gateway:   "172.17.0.1",
				},
			},
		},
	}

	cfg := buildNetworkingConfig(details, details.Config)

	endpoint, ok := cfg.EndpointsConfig["bridge"]
	if !ok {
		t.Fatal("expected bridge endpoint to be present")
	}

	if endpoint.NetworkID != "net123" {
		t.Errorf("NetworkID = %q, want %q", endpoint.NetworkID, "net123")
	}

	if len(endpoint.Aliases) != 1 || endpoint.Aliases[0] != "web" {
		t.Errorf("Aliases = %v, want [web]", endpoint.Aliases)
	}

	// Runtime-only address fields must be cleared.
	if endpoint.IPAddress != "" || endpoint.Gateway != "" {
		t.Errorf("expected runtime address fields to be cleared, got IP=%q Gateway=%q", endpoint.IPAddress, endpoint.Gateway)
	}
}

func TestBuildNetworkingConfigNoNetworks(t *testing.T) {
	details := container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{ID: "abc"},
		Config:            &container.Config{},
		NetworkSettings:   &container.NetworkSettings{},
	}

	cfg := buildNetworkingConfig(details, details.Config)
	if len(cfg.EndpointsConfig) != 0 {
		t.Errorf("expected empty endpoints config, got %v", cfg.EndpointsConfig)
	}
}
