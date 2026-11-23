package gui

import (
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func TestConfigHeadingRoundTrip(t *testing.T) {
	for _, title := range []string{configSectionGeneral, configSectionNetwork, configSectionFull} {
		got, ok := parseConfigHeading(configHeading(title))
		assert.True(t, ok)
		assert.Equal(t, title, got)
	}

	for _, line := range []string{"", "Network", "── ", "  ── Network ──", "──Network──"} {
		_, ok := parseConfigHeading(line)
		assert.False(t, ok, line)
	}
}

func TestConfigJumpTargets(t *testing.T) {
	lines := []string{
		"Press g to jump to a section.",
		"",
		configHeading(configSectionGeneral),
		"ID:             abc",
		// looks like a block of the full details, but isn't inside them
		"Networks:",
		"",
		configHeading(configSectionNetwork),
		"Mode:           bridge",
		"",
		configHeading(configSectionFull),
		"AppArmorProfile: \"\"",
		"Config:",
		"  Labels:",
		"    foo: bar",
		"HostConfig:   ",
		"  NetworkMode: bridge",
		"NetworkSettings:",
	}

	assert.Equal(t, []configJumpTarget{
		{label: "General", line: 2},
		{label: "Network", line: 6},
		{label: "Full details", line: 9},
		{label: "Full details: Config", line: 11},
		{label: "Full details: HostConfig", line: 14},
		{label: "Full details: NetworkSettings", line: 16},
	}, configJumpTargets(lines))
}

func TestContainerConfigSections(t *testing.T) {
	details := container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{
			ID:           "abcdef",
			Image:        "sha256:1234",
			Path:         "nginx",
			Args:         []string{"-g", "daemon off;"},
			RestartCount: 2,
			State:        &container.State{Status: "exited", ExitCode: 137, Health: &container.Health{Status: "unhealthy"}},
			HostConfig: &container.HostConfig{
				NetworkMode:   "shop_default",
				RestartPolicy: container.RestartPolicy{Name: "on-failure", MaximumRetryCount: 3},
				DNS:           []string{"1.1.1.1"},
				Resources:     container.Resources{Memory: 512 * 1024 * 1024, NanoCPUs: 1500000000},
			},
		},
		Config: &container.Config{
			Image:       "nginx:alpine",
			Hostname:    "web",
			Healthcheck: &container.HealthConfig{Test: []string{"CMD", "curl", "localhost"}},
			Labels: map[string]string{
				"com.docker.compose.project":              "shop",
				"com.docker.compose.service":              "web",
				"com.docker.compose.project.config_files": "/srv/shop/compose.yml",
				"a.label": "1",
			},
		},
		Mounts: []container.MountPoint{
			{Type: "volume", Name: "shop_data", Source: "/var/lib/docker/volumes/shop_data/_data", Destination: "/data", RW: true},
			{Type: "bind", Source: "/srv/conf", Destination: "/etc/nginx/conf.d"},
		},
		NetworkSettings: &container.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"shop_default": {IPAddress: "172.20.0.3", IPPrefixLen: 16, Gateway: "172.20.0.1", MacAddress: "02:42:ac:14:00:03", Aliases: []string{"web"}},
				"edge":         {IPAddress: "172.21.0.5", IPPrefixLen: 24},
			},
		},
	}

	details.NetworkSettings.Ports = nat.PortMap{
		"80/tcp":  {{HostIP: "0.0.0.0", HostPort: "8080"}},
		"443/tcp": nil,
	}

	sections := containerConfigSections("shop-web-1", details)

	titles := []string{}
	bodies := map[string]string{}
	for _, section := range sections {
		titles = append(titles, section.title)
		bodies[section.title] = utils.Decolorise(section.body)
	}
	assert.Equal(t, []string{"General", "Network", "Mounts", "Runtime", "Compose", "Labels"}, titles)

	assert.Contains(t, bodies["General"], "Name:           shop-web-1\n")
	assert.Contains(t, bodies["General"], "Command:        nginx -g daemon off;\n")
	assert.Contains(t, bodies["General"], "State:          exited (exit code 137)\n")

	assert.Contains(t, bodies["Network"], "Mode:           shop_default\n")
	assert.Contains(t, bodies["Network"], "shop_default: ip 172.20.0.3/16  gateway 172.20.0.1  mac 02:42:ac:14:00:03  aliases web\n")
	assert.Contains(t, bodies["Network"], "0.0.0.0:8080: 80/tcp\n")
	assert.Contains(t, bodies["Network"], "443/tcp (not published)\n")
	assert.Contains(t, bodies["Network"], "DNS:            1.1.1.1\n")
	// networks and ports come out of maps, so they have to be sorted to be stable
	assert.Less(t, strings.Index(bodies["Network"], "edge:"), strings.Index(bodies["Network"], "shop_default:"))

	assert.Equal(t, "volume: shop_data -> /data\nbind: /srv/conf -> /etc/nginx/conf.d (read-only)\n", bodies["Mounts"])

	assert.Contains(t, bodies["Runtime"], "Restart policy: on-failure (max 3 retries)\n")
	assert.Contains(t, bodies["Runtime"], "Memory limit:   512.00MiB\n")
	assert.Contains(t, bodies["Runtime"], "CPU limit:      1.50\n")
	assert.Contains(t, bodies["Runtime"], "Health:         unhealthy\n")
	assert.Contains(t, bodies["Runtime"], "Health check:   CMD curl localhost\n")

	assert.Contains(t, bodies["Compose"], "Project:        shop\n")
	assert.Contains(t, bodies["Labels"], "a.label: 1\n")
}

func TestContainerConfigSectionsStandalone(t *testing.T) {
	details := container.InspectResponse{ContainerJSONBase: &container.ContainerJSONBase{ID: "abc"}}

	titles := []string{}
	for _, section := range containerConfigSections("solo", details) {
		titles = append(titles, section.title)
	}

	// no compose section, and nothing panics when most of the details are missing
	assert.Equal(t, []string{"General", "Network", "Mounts", "Runtime", "Labels"}, titles)
}
