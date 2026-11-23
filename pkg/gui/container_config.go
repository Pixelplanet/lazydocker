package gui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/gui/types"
	"github.com/jesseduffield/lazydocker/pkg/tasks"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

const (
	configLabelWidth   = 16
	configHeadingWidth = 60
	configHeadingRule  = "─"

	configSectionGeneral = "General"
	configSectionNetwork = "Network"
	configSectionMounts  = "Mounts"
	configSectionRuntime = "Runtime"
	configSectionCompose = "Compose"
	configSectionLabels  = "Labels"
	configSectionFull    = "Full details"
)

// configSection is one titled block of the container Config tab.
type configSection struct {
	title string
	body  string
}

// configJumpTarget is somewhere in the Config tab that can be scrolled to.
type configJumpTarget struct {
	label string
	// line is the index of the target's first line among the lines of the view,
	// after wrapping.
	line int
}

// A block of the full details is a top-level YAML key with nested content.
var configFullDetailsBlock = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*:$`)

// configHeading renders a section title as a ruled line. Jumping to a section
// finds it again by this shape, see parseConfigHeading.
func configHeading(title string) string {
	heading := strings.Repeat(configHeadingRule, 2) + " " + title + " "
	if padding := configHeadingWidth - len([]rune(heading)); padding > 0 {
		heading += strings.Repeat(configHeadingRule, padding)
	}
	return heading
}

// parseConfigHeading returns the title of a line written by configHeading.
func parseConfigHeading(line string) (string, bool) {
	prefix := strings.Repeat(configHeadingRule, 2) + " "
	if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(strings.TrimRight(line, " "), configHeadingRule) {
		return "", false
	}

	title := strings.TrimSpace(strings.Trim(line, configHeadingRule+" "))
	return title, title != ""
}

// configJumpTargets finds the places that can be jumped to in the lines of a
// rendered Config tab: every section, and every block of the full details.
func configJumpTargets(lines []string) []configJumpTarget {
	targets := []configJumpTarget{}
	inFullDetails := false

	for i, line := range lines {
		if title, ok := parseConfigHeading(line); ok {
			targets = append(targets, configJumpTarget{label: title, line: i})
			inFullDetails = title == configSectionFull
			continue
		}

		if line = strings.TrimRight(line, " "); inFullDetails && configFullDetailsBlock.MatchString(line) {
			targets = append(targets, configJumpTarget{
				label: configSectionFull + ": " + strings.TrimSuffix(line, ":"),
				line:  i,
			})
		}
	}

	return targets
}

func configField(label, value string) string {
	return utils.WithPadding(label+":", configLabelWidth) + value + "\n"
}

// configList renders a labelled list with one item per line, or "none".
func configList(label string, items []string) string {
	if len(items) == 0 {
		return configField(label, "none")
	}

	indent := strings.Repeat(" ", configLabelWidth)
	return label + ":\n" + indent + strings.Join(items, "\n"+indent) + "\n"
}

func configKey(key string) string {
	return utils.ColoredString(key+":", color.FgYellow)
}

func (gui *Gui) renderContainerConfig(container *commands.Container) tasks.TaskFunc {
	return gui.NewSimpleRenderStringTask(func() string { return gui.containerConfigStr(container) })
}

func (gui *Gui) containerConfigStr(ctr *commands.Container) string {
	if !ctr.DetailsLoaded() {
		return gui.Tr.WaitingForContainerInfo
	}

	sections := containerConfigSections(ctr.Name, ctr.Details)

	data, err := utils.MarshalIntoYaml(&ctr.Details)
	if err != nil {
		return fmt.Sprintf("Error marshalling container details: %v", err)
	}
	sections = append(sections, configSection{title: configSectionFull, body: utils.ColoredYamlString(string(data))})

	output := utils.ColoredString(gui.Tr.ConfigJumpHint, color.FgBlue) + "\n\n"
	for _, section := range sections {
		heading := utils.MultiColoredString(configHeading(section.title), color.FgCyan, color.Bold)
		output += heading + "\n" + strings.TrimRight(section.body, "\n") + "\n\n"
	}

	return output
}

// containerConfigSections summarises the parts of a container's configuration
// people most often look for, so they don't have to be dug out of the full
// details.
func containerConfigSections(name string, details container.InspectResponse) []configSection {
	sections := []configSection{
		{title: configSectionGeneral, body: containerGeneralConfig(name, details)},
		{title: configSectionNetwork, body: containerNetworkConfig(details)},
		{title: configSectionMounts, body: containerMountsConfig(details)},
		{title: configSectionRuntime, body: containerRuntimeConfig(details)},
	}

	labels := map[string]string{}
	if details.Config != nil {
		labels = details.Config.Labels
	}

	if compose := containerComposeConfig(labels); compose != "" {
		sections = append(sections, configSection{title: configSectionCompose, body: compose})
	}

	return append(sections, configSection{title: configSectionLabels, body: containerLabelsConfig(labels)})
}

func containerGeneralConfig(name string, details container.InspectResponse) string {
	output := configField("ID", details.ID)
	output += configField("Name", name)

	if details.Config != nil {
		output += configField("Image", details.Config.Image)
	}
	output += configField("Image ID", details.Image)
	output += configField("Command", strings.Join(append([]string{details.Path}, details.Args...), " "))
	output += configField("Created", details.Created)

	if state := details.State; state != nil {
		status := state.Status
		if !state.Running {
			status += fmt.Sprintf(" (exit code %d)", state.ExitCode)
		}
		output += configField("State", status)
		output += configField("Started", state.StartedAt)
		if !state.Running {
			output += configField("Finished", state.FinishedAt)
		}
	}

	return output
}

func containerNetworkConfig(details container.InspectResponse) string {
	output := ""

	if details.HostConfig != nil {
		output += configField("Mode", string(details.HostConfig.NetworkMode))
	}
	if details.Config != nil && details.Config.Hostname != "" {
		output += configField("Hostname", details.Config.Hostname)
	}

	networks := []string{}
	ports := []string{}
	if settings := details.NetworkSettings; settings != nil {
		names := make([]string, 0, len(settings.Networks))
		for name := range settings.Networks {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			endpoint := settings.Networks[name]
			if endpoint == nil {
				continue
			}

			properties := []string{}
			if endpoint.IPAddress != "" {
				properties = append(properties, fmt.Sprintf("ip %s/%d", endpoint.IPAddress, endpoint.IPPrefixLen))
			}
			if endpoint.GlobalIPv6Address != "" {
				properties = append(properties, fmt.Sprintf("ipv6 %s/%d", endpoint.GlobalIPv6Address, endpoint.GlobalIPv6PrefixLen))
			}
			if endpoint.Gateway != "" {
				properties = append(properties, "gateway "+endpoint.Gateway)
			}
			if endpoint.MacAddress != "" {
				properties = append(properties, "mac "+endpoint.MacAddress)
			}
			if len(endpoint.Aliases) > 0 {
				properties = append(properties, "aliases "+strings.Join(endpoint.Aliases, ", "))
			}

			networks = append(networks, strings.TrimRight(configKey(name)+" "+strings.Join(properties, "  "), " "))
		}

		containerPorts := make([]string, 0, len(settings.Ports))
		for port := range settings.Ports {
			containerPorts = append(containerPorts, string(port))
		}
		sort.Strings(containerPorts)

		for _, port := range containerPorts {
			bindings := settings.Ports[nat.Port(port)]
			if len(bindings) == 0 {
				ports = append(ports, port+" (not published)")
				continue
			}
			for _, binding := range bindings {
				host := binding.HostPort
				if binding.HostIP != "" {
					host = binding.HostIP + ":" + host
				}
				ports = append(ports, configKey(host)+" "+port)
			}
		}
	}

	output += configList("Networks", networks)
	output += configList("Ports", ports)

	if details.HostConfig != nil {
		if len(details.HostConfig.DNS) > 0 {
			output += configField("DNS", strings.Join(details.HostConfig.DNS, ", "))
		}
		if len(details.HostConfig.ExtraHosts) > 0 {
			output += configList("Extra hosts", details.HostConfig.ExtraHosts)
		}
	}

	return output
}

func containerMountsConfig(details container.InspectResponse) string {
	if len(details.Mounts) == 0 {
		return "none\n"
	}

	output := ""
	for _, mount := range details.Mounts {
		source := mount.Source
		if mount.Type == "volume" && mount.Name != "" {
			source = mount.Name
		}

		line := fmt.Sprintf("%s %s -> %s", configKey(string(mount.Type)), source, mount.Destination)
		if !mount.RW {
			line += " (read-only)"
		}
		output += line + "\n"
	}

	return output
}

func containerRuntimeConfig(details container.InspectResponse) string {
	output := ""

	if hostConfig := details.HostConfig; hostConfig != nil {
		policy := string(hostConfig.RestartPolicy.Name)
		if policy == "" {
			policy = "no"
		}
		if hostConfig.RestartPolicy.MaximumRetryCount > 0 {
			policy += fmt.Sprintf(" (max %d retries)", hostConfig.RestartPolicy.MaximumRetryCount)
		}
		output += configField("Restart policy", policy)
		output += configField("Restart count", fmt.Sprint(details.RestartCount))

		memory := "unlimited"
		if hostConfig.Memory > 0 {
			memory = utils.FormatBinaryBytes(int(hostConfig.Memory))
		}
		output += configField("Memory limit", memory)

		cpus := "unlimited"
		if hostConfig.NanoCPUs > 0 {
			cpus = fmt.Sprintf("%.2f", float64(hostConfig.NanoCPUs)/1e9)
		}
		output += configField("CPU limit", cpus)

		if hostConfig.Privileged {
			output += configField("Privileged", "yes")
		}
	}

	if config := details.Config; config != nil {
		if config.User != "" {
			output += configField("User", config.User)
		}
		if config.WorkingDir != "" {
			output += configField("Working dir", config.WorkingDir)
		}

		health := "no health check"
		if details.State != nil && details.State.Health != nil {
			health = details.State.Health.Status
		}
		output += configField("Health", health)
		if config.Healthcheck != nil && len(config.Healthcheck.Test) > 0 {
			output += configField("Health check", strings.Join(config.Healthcheck.Test, " "))
		}
	}

	return output
}

// containerComposeConfig describes where a compose-managed container comes
// from. It returns an empty string for standalone containers.
func containerComposeConfig(labels map[string]string) string {
	project, ok := labels["com.docker.compose.project"]
	if !ok {
		return ""
	}

	output := configField("Project", project)
	output += configField("Service", labels["com.docker.compose.service"])
	if dir := labels["com.docker.compose.project.working_dir"]; dir != "" {
		output += configField("Working dir", dir)
	}
	if files := labels["com.docker.compose.project.config_files"]; files != "" {
		output += configList("Compose files", strings.Split(files, ","))
	}

	return output
}

func containerLabelsConfig(labels map[string]string) string {
	if len(labels) == 0 {
		return "none\n"
	}

	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	output := ""
	for _, key := range keys {
		output += configKey(key) + " " + labels[key] + "\n"
	}

	return output
}

// containerConfigShown reports whether the main panel is showing a container's
// Config tab, either from the containers panel or from the services panel.
func (gui *Gui) containerConfigShown() bool {
	view := gui.g.CurrentView()
	if view == gui.Views.Main {
		view = view.ParentView
	}

	switch view {
	case gui.Panels.Containers.View:
		return gui.Panels.Containers.ContextState.GetCurrentMainTab().Key == "config"
	case gui.Panels.Services.View:
		return gui.Panels.Services.ContextState.GetCurrentMainTab().Key == "container-config"
	}

	return false
}

// handleConfigJump opens a menu of the sections of the Config tab and scrolls
// to the one that gets picked, so that finding something further down the page
// doesn't take a lot of scrolling.
func (gui *Gui) handleConfigJump(g *gocui.Gui, v *gocui.View) error {
	if !gui.containerConfigShown() {
		gui.flashStatus(gui.Tr.ConfigJumpUnavailable)
		return nil
	}

	targets := configJumpTargets(gui.Views.Main.ViewBufferLines())
	if len(targets) == 0 {
		return nil
	}

	menuItems := make([]*types.MenuItem, len(targets))
	for i, target := range targets {
		label := target.label
		menuItems[i] = &types.MenuItem{
			LabelColumns: []string{label},
			OnPress:      func() error { return gui.jumpToConfigTarget(label) },
		}
	}

	return gui.Menu(CreateMenuOptions{Title: gui.Tr.ConfigJumpTitle, Items: menuItems})
}

func (gui *Gui) jumpToConfigTarget(label string) error {
	mainView := gui.Views.Main

	// Look the target up again rather than remembering its line: the page may
	// have been wrapped differently since the menu was opened.
	for _, target := range configJumpTargets(mainView.ViewBufferLines()) {
		if target.label == label {
			mainView.Autoscroll = false
			return mainView.SetOrigin(0, target.line)
		}
	}

	return nil
}
