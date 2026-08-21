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
// and/or replacing its environment.
//
// Recreation always goes through the Docker SDK, which recreates the container
// with its exact current name (and full config: env, volumes, ports, networks,
// labels, restart policy). Preserving the name matters because other services
// — such as an nginx reverse proxy — may route traffic to the container by
// name. Driving docker compose instead would rename the container to compose's
// canonical "<project>-<service>-<number>" form whenever the current name
// differs from it (for example after a manual rename, or when the container was
// created by an older compose version that used underscores).
func (gui *Gui) recreateContainer(ctr *commands.Container, pull bool, newEnv []string) error {
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
