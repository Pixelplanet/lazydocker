package gui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/gui/types"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

// splitEnvVar splits a "KEY=VALUE" environment string into its key and value.
func splitEnvVar(envVar string) (string, string) {
	parts := strings.SplitN(envVar, "=", 2)
	key := parts[0]
	value := ""
	if len(parts) > 1 {
		value = parts[1]
	}
	return key, value
}

// truncateString shortens s to at most maxLen runes, appending an ellipsis when
// truncation occurs. Used to keep menu labels readable.
func truncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}

// containerIsComposeManaged reports whether the container is managed by docker
// compose (as opposed to being a standalone container).
func containerIsComposeManaged(ctr *commands.Container) bool {
	return ctr.IsComposeManaged()
}

// composeFilesAccessible reports whether the compose config files referenced by
// the container's labels exist on this machine. When they don't (for example,
// the container was created on a different host), we cannot drive docker
// compose and must fall back to recreating via the Docker SDK.
func composeFilesAccessible(info commands.ComposeInfo) bool {
	if len(info.ConfigFiles) == 0 {
		return false
	}
	for _, f := range info.ConfigFiles {
		if _, err := os.Stat(strings.TrimSpace(f)); err != nil {
			return false
		}
	}
	return true
}

// containerImageName returns the best-known image reference for the container.
func containerImageName(ctr *commands.Container) string {
	if ctr.DetailsLoaded() && ctr.Details.Config != nil && ctr.Details.Config.Image != "" {
		return ctr.Details.Config.Image
	}
	return ctr.Container.Image
}

// confirmPrompt appends a short hint to a confirmation prompt so the user can
// always see how to confirm or cancel, regardless of the bottom options bar.
func (gui *Gui) confirmPrompt(prompt string) string {
	return prompt + "\n\n" + gui.Tr.ConfirmationHint
}

// copyToClipboard copies text to the clipboard (via OSC52) and flashes a status.
func (gui *Gui) copyToClipboard(text string) error {
	if text == "" {
		gui.flashStatus(gui.Tr.NothingToCopy)
		return nil
	}

	if err := utils.CopyToClipboard(text); err != nil {
		return gui.createErrorPanel(err.Error())
	}

	gui.flashStatus(gui.Tr.Copied)
	return nil
}

// --- Copy menu ---

func (gui *Gui) handleContainersCopyMenu(g *gocui.Gui, v *gocui.View) error {
	ctr, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.createCopyMenu(ctr)
}

func (gui *Gui) createCopyMenu(ctr *commands.Container) error {
	menuItems := []*types.MenuItem{
		{
			LabelColumns: []string{gui.Tr.CopyContainerID},
			OnPress:      func() error { return gui.copyToClipboard(ctr.ID) },
		},
		{
			LabelColumns: []string{gui.Tr.CopyContainerName},
			OnPress:      func() error { return gui.copyToClipboard(ctr.Name) },
		},
		{
			LabelColumns: []string{gui.Tr.CopyContainerImage},
			OnPress:      func() error { return gui.copyToClipboard(containerImageName(ctr)) },
		},
		{
			LabelColumns: []string{gui.Tr.CopyEnvVariables},
			OnPress:      func() error { return gui.createEnvCopyMenu(ctr) },
		},
		{
			LabelColumns: []string{gui.Tr.ExportEnvToFile},
			OnPress:      func() error { return gui.exportEnvToFile(ctr) },
		},
	}

	return gui.Menu(CreateMenuOptions{Title: gui.Tr.CopyMenuTitle, Items: menuItems})
}

// createEnvCopyMenu lets the user select an environment variable to copy.
func (gui *Gui) createEnvCopyMenu(ctr *commands.Container) error {
	envVars := ctr.EnvVars()
	if len(envVars) == 0 {
		gui.flashStatus(gui.Tr.NothingToCopy)
		return nil
	}

	menuItems := make([]*types.MenuItem, 0, len(envVars))
	for _, envVar := range envVars {
		key, value := splitEnvVar(envVar)
		pair := envVar
		v := value
		k := key
		menuItems = append(menuItems, &types.MenuItem{
			LabelColumns: []string{k, truncateString(v, 60)},
			OnPress:      func() error { return gui.createEnvVarCopyMenu(k, v, pair) },
		})
	}

	return gui.Menu(CreateMenuOptions{Title: gui.Tr.CopyEnvVariableTitle, Items: menuItems})
}

func (gui *Gui) createEnvVarCopyMenu(key, value, pair string) error {
	menuItems := []*types.MenuItem{
		{
			LabelColumns: []string{gui.Tr.CopyEnvValue, truncateString(value, 60)},
			OnPress:      func() error { return gui.copyToClipboard(value) },
		},
		{
			LabelColumns: []string{gui.Tr.CopyEnvKeyValue, truncateString(pair, 60)},
			OnPress:      func() error { return gui.copyToClipboard(pair) },
		},
	}

	return gui.Menu(CreateMenuOptions{Title: key, Items: menuItems})
}

// handleMainCopy handles the copy keybinding while the main panel is focused.
// When viewing a container's Env tab it opens the env-var copy menu, otherwise
// it copies the whole content of the main view.
func (gui *Gui) handleMainCopy(g *gocui.Gui, v *gocui.View) error {
	if gui.Views.Main.ParentView == gui.Panels.Containers.View &&
		gui.Panels.Containers.ContextState.GetCurrentMainTab().Key == "env" {
		ctr, err := gui.Panels.Containers.GetSelectedItem()
		if err != nil {
			return nil
		}
		return gui.createEnvCopyMenu(ctr)
	}

	return gui.copyToClipboard(strings.TrimRight(v.Buffer(), "\n"))
}

// exportEnvToFile writes all of the container's env vars to a .env file in the
// user's home directory and copies the resulting path to the clipboard.
func (gui *Gui) exportEnvToFile(ctr *commands.Container) error {
	envVars := ctr.EnvVars()
	if len(envVars) == 0 {
		gui.flashStatus(gui.Tr.NothingToCopy)
		return nil
	}

	dir, err := os.UserHomeDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}

	safeName := strings.TrimPrefix(ctr.Name, "/")
	if safeName == "" {
		safeName = ctr.ID
	}
	path := filepath.Join(dir, safeName+".env")

	content := strings.Join(envVars, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return gui.createErrorPanel(err.Error())
	}

	_ = utils.CopyToClipboard(path)
	gui.flashStatus(fmt.Sprintf("%s %s", gui.Tr.ExportedEnvTo, path))
	return nil
}

// --- Container actions menu ---

func (gui *Gui) handleContainersActionsMenu(g *gocui.Gui, v *gocui.View) error {
	ctr, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.createContainerActionsMenu(ctr)
}

func (gui *Gui) createContainerActionsMenu(ctr *commands.Container) error {
	menuItems := []*types.MenuItem{
		{
			LabelColumns: []string{gui.Tr.UpdateAndRecreate},
			OnPress:      func() error { return gui.recreateContainer(ctr, true, nil) },
		},
		{
			LabelColumns: []string{gui.Tr.RecreateKeepImage},
			OnPress:      func() error { return gui.recreateContainer(ctr, false, nil) },
		},
		{
			LabelColumns: []string{gui.Tr.EditEnvAndRecreate},
			OnPress:      func() error { return gui.handleEditEnvAndRecreate(ctr) },
		},
		{
			LabelColumns: []string{gui.Tr.StartContainer},
			OnPress:      func() error { return gui.startContainer(ctr) },
		},
		{
			LabelColumns: []string{gui.Tr.Rename},
			OnPress:      func() error { return gui.handleRenameContainer(ctr) },
		},
		{
			LabelColumns: []string{gui.Tr.ChangeRestartPolicy},
			OnPress:      func() error { return gui.createRestartPolicyMenu(ctr) },
		},
	}

	return gui.Menu(CreateMenuOptions{Title: gui.Tr.ContainerActionsTitle, Items: menuItems})
}

// recreateContainer recreates a container, optionally pulling the latest image
// and/or replacing its environment. Compose-managed containers are recreated
// through docker compose so the compose file remains the source of truth,
// falling back to the Docker SDK when the compose files are not accessible.
func (gui *Gui) recreateContainer(ctr *commands.Container, pull bool, newEnv []string) error {
	if containerIsComposeManaged(ctr) {
		info, _ := ctr.ComposeInfo()
		if composeFilesAccessible(info) {
			return gui.recreateComposeContainer(ctr, info, pull)
		}
		// Compose files aren't reachable from this machine, so fall through to
		// the SDK recreate which works regardless of the compose file location.
	}

	prompt := gui.Tr.ConfirmRecreate
	if pull {
		prompt = gui.Tr.ConfirmUpdateAndRecreate
	}

	return gui.createConfirmationPanel(gui.Tr.Confirm, gui.confirmPrompt(prompt), func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.RecreatingStatus, func() error {
			if err := ctr.Recreate(context.Background(), commands.RecreateOptions{PullImage: pull, NewEnv: newEnv}); err != nil {
				return gui.createErrorPanel(err.Error())
			}
			return gui.refreshContainersAndServices()
		})
	}, nil)
}

// composeCommandPrefix builds the docker compose invocation targeting the
// container's original project directory and config files, so it works even
// when lazydocker was launched from a different directory.
func (gui *Gui) composeCommandPrefix(info commands.ComposeInfo) string {
	base := gui.Config.UserConfig.CommandTemplates.DockerCompose

	var b strings.Builder
	b.WriteString(base)
	b.WriteString(fmt.Sprintf(" -p %s", info.Project))
	if info.WorkingDir != "" {
		b.WriteString(fmt.Sprintf(" --project-directory %s", gui.OSCommand.Quote(info.WorkingDir)))
	}
	for _, f := range info.ConfigFiles {
		b.WriteString(fmt.Sprintf(" -f %s", gui.OSCommand.Quote(strings.TrimSpace(f))))
	}

	return b.String()
}

// recreateComposeContainer pulls (optionally) and recreates a compose-managed
// container via docker compose, using the compose files referenced by the
// container's labels.
func (gui *Gui) recreateComposeContainer(ctr *commands.Container, info commands.ComposeInfo, pull bool) error {
	composeBase := gui.composeCommandPrefix(info)

	prompt := gui.Tr.ConfirmRecreate
	if pull {
		prompt = gui.Tr.ConfirmUpdateAndRecreate
	}

	return gui.createConfirmationPanel(gui.Tr.Confirm, gui.confirmPrompt(prompt), func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.RecreatingStatus, func() error {
			if pull {
				pullCmd := fmt.Sprintf("%s pull %s", composeBase, info.Service)
				if err := gui.OSCommand.RunCommand(pullCmd); err != nil {
					return gui.createErrorPanel(err.Error())
				}
			}

			upCmd := fmt.Sprintf("%s up -d --force-recreate %s", composeBase, info.Service)
			if err := gui.OSCommand.RunCommand(upCmd); err != nil {
				return gui.createErrorPanel(err.Error())
			}

			return gui.refreshContainersAndServices()
		})
	}, nil)
}

func (gui *Gui) startContainer(ctr *commands.Container) error {
	return gui.WithWaitingStatus(gui.Tr.StartingStatus, func() error {
		if err := ctr.Start(); err != nil {
			return gui.createErrorPanel(err.Error())
		}
		return gui.refreshContainersAndServices()
	})
}

func (gui *Gui) handleRenameContainer(ctr *commands.Container) error {
	currentName := strings.TrimPrefix(ctr.Name, "/")

	return gui.createPromptPanelWithDefault(gui.Tr.RenameContainerPrompt, currentName, func(g *gocui.Gui, v *gocui.View) error {
		newName := gui.trimmedContent(v)
		if newName == "" || newName == currentName {
			return nil
		}

		return gui.WithWaitingStatus(gui.Tr.RenamingStatus, func() error {
			if err := ctr.Rename(newName); err != nil {
				return gui.createErrorPanel(err.Error())
			}
			return gui.refreshContainersAndServices()
		})
	})
}

func (gui *Gui) createRestartPolicyMenu(ctr *commands.Container) error {
	setPolicy := func(mode container.RestartPolicyMode) func() error {
		return func() error {
			return gui.WithWaitingStatus(gui.Tr.UpdatingRestartPolicyStatus, func() error {
				if err := ctr.UpdateRestartPolicy(mode); err != nil {
					return gui.createErrorPanel(err.Error())
				}
				return gui.refreshContainersAndServices()
			})
		}
	}

	menuItems := []*types.MenuItem{
		{LabelColumns: []string{gui.Tr.RestartPolicyNo}, OnPress: setPolicy(container.RestartPolicyDisabled)},
		{LabelColumns: []string{gui.Tr.RestartPolicyOnFailure}, OnPress: setPolicy(container.RestartPolicyOnFailure)},
		{LabelColumns: []string{gui.Tr.RestartPolicyAlways}, OnPress: setPolicy(container.RestartPolicyAlways)},
		{LabelColumns: []string{gui.Tr.RestartPolicyUnlessStopped}, OnPress: setPolicy(container.RestartPolicyUnlessStopped)},
	}

	return gui.Menu(CreateMenuOptions{Title: gui.Tr.RestartPolicyMenuTitle, Items: menuItems})
}

// --- In-app environment editing ---

func (gui *Gui) handleEditEnvAndRecreate(ctr *commands.Container) error {
	if containerIsComposeManaged(ctr) {
		return gui.createErrorPanel(gui.Tr.CannotEditComposeEnv)
	}

	envVars := append([]string(nil), ctr.EnvVars()...)
	return gui.showEnvEditMenu(ctr, &envVars)
}

func (gui *Gui) showEnvEditMenu(ctr *commands.Container, envVars *[]string) error {
	menuItems := make([]*types.MenuItem, 0, len(*envVars)+3)

	for i := range *envVars {
		idx := i
		key, value := splitEnvVar((*envVars)[idx])
		menuItems = append(menuItems, &types.MenuItem{
			LabelColumns: []string{key, truncateString(value, 50)},
			OnPress:      func() error { return gui.promptEditEnvVar(ctr, envVars, idx) },
		})
	}

	menuItems = append(menuItems,
		&types.MenuItem{
			LabelColumns: []string{gui.Tr.AddEnvVariable},
			OnPress:      func() error { return gui.promptAddEnvVar(ctr, envVars) },
		},
		&types.MenuItem{
			LabelColumns: []string{gui.Tr.ApplyAndRecreatePull},
			OnPress:      func() error { return gui.applyEnvAndRecreate(ctr, *envVars, true) },
		},
		&types.MenuItem{
			LabelColumns: []string{gui.Tr.ApplyAndRecreate},
			OnPress:      func() error { return gui.applyEnvAndRecreate(ctr, *envVars, false) },
		},
	)

	return gui.Menu(CreateMenuOptions{Title: gui.Tr.EditEnvMenuTitle, Items: menuItems})
}

func (gui *Gui) promptEditEnvVar(ctr *commands.Container, envVars *[]string, idx int) error {
	key, value := splitEnvVar((*envVars)[idx])
	title := fmt.Sprintf("%s %s:", gui.Tr.EditEnvVarPrompt, key)

	return gui.createPromptPanelWithDefault(title, value, func(g *gocui.Gui, v *gocui.View) error {
		newValue := gui.trimmedContent(v)
		(*envVars)[idx] = key + "=" + newValue
		return gui.showEnvEditMenu(ctr, envVars)
	})
}

func (gui *Gui) promptAddEnvVar(ctr *commands.Container, envVars *[]string) error {
	return gui.createPromptPanel(gui.Tr.AddEnvVarPrompt, func(g *gocui.Gui, v *gocui.View) error {
		entry := gui.trimmedContent(v)
		if entry != "" && strings.Contains(entry, "=") {
			*envVars = append(*envVars, entry)
		}
		return gui.showEnvEditMenu(ctr, envVars)
	})
}

func (gui *Gui) applyEnvAndRecreate(ctr *commands.Container, envVars []string, pull bool) error {
	return gui.WithWaitingStatus(gui.Tr.RecreatingStatus, func() error {
		if err := ctr.Recreate(context.Background(), commands.RecreateOptions{PullImage: pull, NewEnv: envVars}); err != nil {
			return gui.createErrorPanel(err.Error())
		}
		return gui.refreshContainersAndServices()
	})
}

// --- Bulk update ---

func (gui *Gui) handleUpdateAllContainers() error {
	return gui.createConfirmationPanel(gui.Tr.Confirm, gui.confirmPrompt(gui.Tr.ConfirmUpdateAllContainers), func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.UpdatingAllContainersStatus, func() error {
			for _, ctr := range gui.Panels.Containers.List.GetAllItems() {
				if containerIsComposeManaged(ctr) {
					continue
				}
				if err := ctr.Recreate(context.Background(), commands.RecreateOptions{PullImage: true}); err != nil {
					gui.Log.Error(err)
				}
			}
			return gui.refreshContainersAndServices()
		})
	}, nil)
}
