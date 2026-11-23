package commands

import (
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/assert"
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

func TestComposeInfo(t *testing.T) {
	ctr := &Container{
		Container: container.Summary{
			Labels: map[string]string{
				"com.docker.compose.project":              "cloudify-3d",
				"com.docker.compose.service":              "backend",
				"com.docker.compose.project.working_dir":  `C:\Projects\cloudify-3d`,
				"com.docker.compose.project.config_files": `C:\Projects\cloudify-3d\docker-compose.yml,C:\Projects\cloudify-3d\docker-compose.override.yml`,
			},
		},
	}

	if !ctr.IsComposeManaged() {
		t.Fatal("expected IsComposeManaged to be true")
	}

	info, ok := ctr.ComposeInfo()
	if !ok {
		t.Fatal("expected ComposeInfo to return ok")
	}
	if info.Project != "cloudify-3d" {
		t.Errorf("Project = %q, want cloudify-3d", info.Project)
	}
	if info.Service != "backend" {
		t.Errorf("Service = %q, want backend", info.Service)
	}
	if info.WorkingDir != `C:\Projects\cloudify-3d` {
		t.Errorf("WorkingDir = %q", info.WorkingDir)
	}
	if len(info.ConfigFiles) != 2 {
		t.Fatalf("expected 2 config files, got %v", info.ConfigFiles)
	}
	if info.ConfigFiles[1] != `C:\Projects\cloudify-3d\docker-compose.override.yml` {
		t.Errorf("second config file = %q", info.ConfigFiles[1])
	}
}

func TestComposeBaseCommand(t *testing.T) {
	scenarios := []struct {
		template string
		expected []string
	}{
		{"docker compose", []string{"docker", "compose"}},
		{"docker-compose", []string{"docker-compose"}},
		{"docker compose -f one.yml -f two.yml", []string{"docker", "compose"}},
		{"docker compose --file=one.yml -p local --project-directory /srv/app --ansi never", []string{"docker", "compose", "--ansi", "never"}},
		{"", []string{"docker", "compose"}},
	}

	for _, s := range scenarios {
		t.Run(s.template, func(t *testing.T) {
			assert.Equal(t, s.expected, composeBaseCommand(s.template))
		})
	}
}

func TestComposeArgs(t *testing.T) {
	info := ComposeInfo{
		Project:     "shop",
		Service:     "web",
		WorkingDir:  "/srv/my shop",
		ConfigFiles: []string{"/srv/my shop/compose.yml", "/srv/my shop/compose.prod.yml"},
		EnvFiles:    []string{"/srv/my shop/prod.env"},
	}

	// The -f from the template belongs to the project lazydocker was launched
	// in, not to the container's project, so it must not be passed on.
	got := composeArgs("docker compose -f local.yml", info, "up", "-d", "--no-deps", "--force-recreate", "web")

	assert.Equal(t, []string{
		"docker", "compose",
		"-p", "shop",
		"--project-directory", "/srv/my shop",
		"-f", "/srv/my shop/compose.yml",
		"-f", "/srv/my shop/compose.prod.yml",
		"--env-file", "/srv/my shop/prod.env",
		"up", "-d", "--no-deps", "--force-recreate", "web",
	}, got)
}

func TestParseComposeConfigHash(t *testing.T) {
	output := "db 1111\nweb 2222\n"

	assert.Equal(t, "2222", parseComposeConfigHash(output, "web"))
	assert.Equal(t, "", parseComposeConfigHash(output, "cache"))
	assert.Equal(t, "", parseComposeConfigHash("", "web"))
}

func TestComposeInfoExtraLabels(t *testing.T) {
	ctr := &Container{
		Container: container.Summary{
			Labels: map[string]string{
				"com.docker.compose.project":                  "shop",
				"com.docker.compose.service":                  "web",
				"com.docker.compose.project.config_files":     "/srv/shop/compose.yml, /srv/shop/compose.prod.yml,",
				"com.docker.compose.project.environment_file": "/srv/shop/prod.env",
				"com.docker.compose.config-hash":              "2222",
				"com.docker.compose.container-number":         "1",
			},
		},
	}

	info, ok := ctr.ComposeInfo()
	assert.True(t, ok)
	assert.Equal(t, []string{"/srv/shop/compose.yml", "/srv/shop/compose.prod.yml"}, info.ConfigFiles)
	assert.Equal(t, []string{"/srv/shop/prod.env"}, info.EnvFiles)
	assert.Equal(t, "2222", info.ConfigHash)
	assert.Equal(t, "1", info.ContainerNumber)
}

func TestSanitizeConfigForRecreate(t *testing.T) {
	const id = "abcdef1234567890"

	t.Run("drops the hostname docker derived from the old id", func(t *testing.T) {
		config := &container.Config{Hostname: "abcdef123456"}
		sanitizeConfigForRecreate(id, config, &container.HostConfig{})
		assert.Equal(t, "", config.Hostname)
	})

	t.Run("keeps a hostname the user chose", func(t *testing.T) {
		config := &container.Config{Hostname: "web"}
		sanitizeConfigForRecreate(id, config, &container.HostConfig{})
		assert.Equal(t, "web", config.Hostname)
	})

	t.Run("drops what a shared network namespace provides", func(t *testing.T) {
		config := &container.Config{Hostname: "other", Domainname: "example.com", ExposedPorts: nat.PortSet{"80/tcp": {}}}
		sanitizeConfigForRecreate(id, config, &container.HostConfig{NetworkMode: "container:other"})
		assert.Equal(t, "", config.Hostname)
		assert.Equal(t, "", config.Domainname)
		assert.Nil(t, config.ExposedPorts)
	})
}

func TestComposeInfoStandalone(t *testing.T) {
	ctr := &Container{
		Container: container.Summary{Labels: map[string]string{"foo": "bar"}},
	}

	if ctr.IsComposeManaged() {
		t.Error("standalone container should not be compose managed")
	}
	if _, ok := ctr.ComposeInfo(); ok {
		t.Error("ComposeInfo should return false for standalone container")
	}
}
