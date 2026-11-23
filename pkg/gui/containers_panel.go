package gui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/gui/panels"
	"github.com/jesseduffield/lazydocker/pkg/gui/presentation"
	"github.com/jesseduffield/lazydocker/pkg/gui/types"
	"github.com/jesseduffield/lazydocker/pkg/tasks"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/samber/lo"
)

func (gui *Gui) getContainersPanel() *panels.SideListPanel[*commands.Container] {
	// Standalone containers are containers which are either one-off containers, or whose service is not part of this docker-compose context.
	isStandaloneContainer := func(container *commands.Container) bool {
		if container.OneOff || container.ServiceName == "" {
			return true
		}

		return !lo.SomeBy(gui.Panels.Services.List.GetAllItems(), func(service *commands.Service) bool {
			return service.Name == container.ServiceName && service.ProjectName == container.ProjectName
		})
	}

	return &panels.SideListPanel[*commands.Container]{
		ContextState: &panels.ContextState[*commands.Container]{
			GetMainTabs: func() []panels.MainTab[*commands.Container] {
				return []panels.MainTab[*commands.Container]{
					{
						Key:    "logs",
						Title:  gui.Tr.LogsTitle,
						Render: gui.renderContainerLogsToMain,
					},
					{
						Key:    "stats",
						Title:  gui.Tr.StatsTitle,
						Render: gui.renderContainerStats,
					},
					{
						Key:    "env",
						Title:  gui.Tr.EnvTitle,
						Render: gui.renderContainerEnv,
					},
					{
						Key:    "config",
						Title:  gui.Tr.ConfigTitle,
						Render: gui.renderContainerConfig,
					},
					{
						Key:    "top",
						Title:  gui.Tr.TopTitle,
						Render: gui.renderContainerTop,
					},
				}
			},
			GetItemContextCacheKey: func(container *commands.Container) string {
				// Including the container state in the cache key so that if the container
				// restarts we re-read the logs. In the past we've had some glitchiness
				// where a container restarts but the new logs don't get read.
				// Note that this might be jarring if we have a lot of logs and the container
				// restarts a lot, so let's keep an eye on it.
				return "containers-" + container.ID + "-" + container.Container.State
			},
		},
		ListPanel: panels.ListPanel[*commands.Container]{
			List: panels.NewFilteredList[*commands.Container](),
			View: gui.Views.Containers,
		},
		NoItemsMessage: gui.Tr.NoContainers,
		Gui:            gui.intoInterface(),
		// sortedContainers returns containers sorted by state if c.SortContainersByState is true (follows 1- running, 2- exited, 3- created)
		// and sorted by name if c.SortContainersByState is false
		Sort: func(a *commands.Container, b *commands.Container) bool {
			return sortContainers(a, b, gui.Config.UserConfig.Gui.LegacySortContainers)
		},
		Filter: func(container *commands.Container) bool {
			if !gui.State.ShowExitedContainers && container.Container.State == "exited" {
				return false
			}

			// When project-scoped, apply project and standalone filtering.
			// Otherwise all containers are shown in a flat list regardless
			// of which compose project they belong to.
			if gui.DockerCommand.IsProjectScoped() {
				// This check must be inside the IsProjectScoped guard: when
				// not project-scoped, services are still derived from container
				// labels, so compose-managed containers from other projects
				// would be incorrectly hidden.
				//
				// Note that this is O(N*M) time complexity where N is the number of services
				// and M is the number of containers. We expect N to be small but M may be large,
				// so we will need to keep an eye on this.
				if !gui.Config.UserConfig.Gui.ShowAllContainers && !isStandaloneContainer(container) {
					return false
				}

				// Filter by selected project. Containers with no project (truly
				// standalone, not from any compose project) are always shown.
				selectedProject := gui.getSelectedProjectName()
				if selectedProject == "" {
					selectedProject = gui.DockerCommand.LocalProjectName
				}
				if selectedProject != "" && container.ProjectName != "" && container.ProjectName != selectedProject {
					return false
				}
			}

			return true
		},
		GetTableCells: func(container *commands.Container) []string {
			return presentation.GetContainerDisplayStrings(&gui.Config.UserConfig.Gui, container)
		},
	}
}

var containerStates = map[string]int{
	"running": 1,
	"exited":  2,
	"created": 3,
}

func sortContainers(a *commands.Container, b *commands.Container, legacySort bool) bool {
	if legacySort {
		return a.Name < b.Name
	}

	stateLeft := containerStates[a.Container.State]
	stateRight := containerStates[b.Container.State]
	if stateLeft == stateRight {
		return a.Name < b.Name
	}

	return containerStates[a.Container.State] < containerStates[b.Container.State]
}

func (gui *Gui) renderContainerEnv(container *commands.Container) tasks.TaskFunc {
	return gui.NewSimpleRenderStringTask(func() string { return gui.containerEnv(container) })
}

func (gui *Gui) containerEnv(container *commands.Container) string {
	if !container.DetailsLoaded() {
		return gui.Tr.WaitingForContainerInfo
	}

	if len(container.Details.Config.Env) == 0 {
		return gui.Tr.NothingToDisplay
	}

	envVarsList := lo.Map(container.Details.Config.Env, func(envVar string, _ int) []string {
		splitEnv := strings.SplitN(envVar, "=", 2)
		key := splitEnv[0]
		value := ""
		if len(splitEnv) > 1 {
			value = splitEnv[1]
		}
		return []string{
			utils.ColoredString(key+":", color.FgGreen),
			utils.ColoredString(value, color.FgYellow),
		}
	})

	output, err := utils.RenderTable(envVarsList)
	if err != nil {
		gui.Log.Error(err)
		return gui.Tr.CannotDisplayEnvVariables
	}

	return output
}

func (gui *Gui) renderContainerStats(container *commands.Container) tasks.TaskFunc {
	return gui.NewTickerTask(TickerTaskOpts{
		Func: func(ctx context.Context, notifyStopped chan struct{}) {
			contents, err := presentation.RenderStats(gui.Config.UserConfig, container, gui.Views.Main.Width())
			if err != nil {
				_ = gui.createErrorPanel(err.Error())
			}

			gui.reRenderStringMain(contents)
		},
		Duration:   time.Second,
		Before:     func(ctx context.Context) { gui.clearMainView() },
		Wrap:       false, // wrapping looks bad here so we're overriding the config value
		Autoscroll: false,
	})
}

func (gui *Gui) renderContainerTop(container *commands.Container) tasks.TaskFunc {
	return gui.NewTickerTask(TickerTaskOpts{
		Func: func(ctx context.Context, notifyStopped chan struct{}) {
			contents, err := container.RenderTop(ctx)
			if err != nil {
				gui.RenderStringMain(err.Error())
			}

			gui.reRenderStringMain(contents)
		},
		Duration:   time.Second,
		Before:     func(ctx context.Context) { gui.clearMainView() },
		Wrap:       gui.Config.UserConfig.Gui.WrapMainPanel,
		Autoscroll: false,
	})
}

func (gui *Gui) refreshContainersAndServices() error {
	if gui.Views.Containers == nil {
		// if the containersView hasn't been instantiated yet we just return
		return nil
	}

	// keep track of current service selected so that we can reposition our cursor if it moves position in the list
	originalSelectedLineIdx := gui.Panels.Services.SelectedIdx
	selectedService, isServiceSelected := gui.Panels.Services.List.TryGet(originalSelectedLineIdx)

	containers, services, err := gui.DockerCommand.RefreshContainersAndServices(
		gui.Panels.Containers.List.GetAllItems(),
	)
	if err != nil {
		return err
	}

	gui.Panels.Services.SetItems(services)
	gui.Panels.Containers.SetItems(containers)

	// see if our selected service has moved
	if isServiceSelected {
		for i, service := range gui.Panels.Services.List.GetItems() {
			if service.ID == selectedService.ID {
				if i == originalSelectedLineIdx {
					break
				}
				gui.Panels.Services.SetSelectedLineIdx(i)
				gui.Panels.Services.Refocus()
			}
		}
	}

	// `docker ps -a` gives us summaries but not the details we display (exit
	// codes, health). Inspect only the containers actually on screen instead of
	// every container on the host.
	if err := gui.DockerCommand.RefreshContainerDetails(gui.containersNeedingDetails()); err != nil {
		gui.Log.Error(err)
	}

	return gui.renderContainersAndServices()
}

func (gui *Gui) renderContainersAndServices() error {
	if err := gui.Panels.Services.RerenderList(); err != nil {
		return err
	}

	if err := gui.Panels.Containers.RerenderList(); err != nil {
		return err
	}

	return nil
}

func (gui *Gui) handleHideStoppedContainers(g *gocui.Gui, v *gocui.View) error {
	gui.State.ShowExitedContainers = !gui.State.ShowExitedContainers

	return gui.Panels.Containers.RerenderList()
}

func (gui *Gui) handleContainersRemoveMenu(g *gocui.Gui, v *gocui.View) error {
	ctr, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	handleMenuPress := func(configOptions container.RemoveOptions) error {
		return gui.WithWaitingStatus(gui.Tr.RemovingStatus, func() error {
			if err := ctr.Remove(configOptions); err != nil {
				if commands.HasErrorCode(err, commands.MustStopContainer) {
					return gui.createConfirmationPanel(gui.Tr.Confirm, gui.containerPrompt(ctr, gui.Tr.MustForceToRemoveContainer), func(g *gocui.Gui, v *gocui.View) error {
						return gui.WithWaitingStatus(gui.Tr.RemovingStatus, func() error {
							configOptions.Force = true
							return ctr.Remove(configOptions)
						})
					}, nil)
				}
				return gui.createErrorPanel(err.Error())
			}
			return nil
		})
	}

	menuItems := []*types.MenuItem{
		{
			LabelColumns: []string{gui.Tr.Remove, "docker rm " + ctr.ID[1:10]},
			OnPress:      func() error { return handleMenuPress(container.RemoveOptions{}) },
		},
		{
			LabelColumns: []string{gui.Tr.RemoveWithVolumes, "docker rm --volumes " + ctr.ID[1:10]},
			OnPress:      func() error { return handleMenuPress(container.RemoveOptions{RemoveVolumes: true}) },
		},
	}

	return gui.Menu(CreateMenuOptions{
		Title: ctr.Name,
		Items: menuItems,
	})
}

func (gui *Gui) PauseContainer(container *commands.Container) error {
	return gui.WithWaitingStatus(gui.Tr.PausingStatus, func() (err error) {
		if container.Container.State == "paused" {
			err = container.Unpause()
		} else {
			err = container.Pause()
		}

		if err != nil {
			return gui.createErrorPanel(err.Error())
		}

		return gui.refreshContainersAndServices()
	})
}

func (gui *Gui) handleContainerPause(g *gocui.Gui, v *gocui.View) error {
	ctr, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.PauseContainer(ctr)
}

func (gui *Gui) handleContainerStop(g *gocui.Gui, v *gocui.View) error {
	ctr, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.createConfirmationPanel(gui.Tr.Confirm, gui.containerPrompt(ctr, gui.Tr.StopContainer), func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.StoppingStatus, func() error {
			if err := ctr.Stop(); err != nil {
				return gui.createErrorPanel(err.Error())
			}

			return nil
		})
	}, nil)
}

func (gui *Gui) handleContainerRestart(g *gocui.Gui, v *gocui.View) error {
	ctr, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.WithWaitingStatus(gui.Tr.RestartingStatus, func() error {
		if err := ctr.Restart(); err != nil {
			return gui.createErrorPanel(err.Error())
		}

		return nil
	})
}

func (gui *Gui) handleContainerAttach(g *gocui.Gui, v *gocui.View) error {
	ctr, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	c, err := ctr.Attach()
	if err != nil {
		return gui.createErrorPanel(err.Error())
	}

	return gui.runSubprocessWithMessage(c, gui.Tr.DetachFromContainerShortCut)
}

func (gui *Gui) handlePruneContainers() error {
	prompt := gui.Tr.ConfirmPruneContainers
	// docker prunes every container that isn't running, paused or restarting
	stopped := lo.Filter(gui.Panels.Containers.List.GetAllItems(), func(ctr *commands.Container, _ int) bool {
		return lo.Contains([]string{"exited", "created", "dead"}, ctr.Container.State)
	})
	if len(stopped) > 0 {
		prompt = gui.containersPrompt(stopped, prompt)
	}

	return gui.createConfirmationPanel(gui.Tr.Confirm, prompt, func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.PruningStatus, func() error {
			err := gui.DockerCommand.PruneContainers()
			if err != nil {
				return gui.createErrorPanel(err.Error())
			}
			return nil
		})
	}, nil)
}

func (gui *Gui) handleContainerViewLogs(g *gocui.Gui, v *gocui.View) error {
	ctr, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	gui.renderLogsToStdout(ctr)

	return nil
}

func (gui *Gui) handleContainersExecShell(g *gocui.Gui, v *gocui.View) error {
	ctr, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.containerExecShell(ctr)
}

func (gui *Gui) containerExecShell(container *commands.Container) error {
	commandObject := gui.DockerCommand.NewCommandObject(commands.CommandObject{
		Container: container,
	})

	// TODO: use SDK
	resolvedCommand := utils.ApplyTemplate("docker exec -it {{ .Container.ID }} /bin/sh -c 'eval $(grep ^$(id -un): /etc/passwd | cut -d : -f 7-)'", commandObject)
	// attach and return the subprocess error
	cmd := gui.OSCommand.ExecutableFromString(resolvedCommand)
	return gui.runSubprocess(cmd)
}

func (gui *Gui) handleContainersCustomCommand(g *gocui.Gui, v *gocui.View) error {
	ctr, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	commandObject := gui.DockerCommand.NewCommandObject(commands.CommandObject{
		Container: ctr,
	})

	customCommands := gui.Config.UserConfig.CustomCommands.Containers

	return gui.createCustomCommandMenu(customCommands, commandObject)
}

func (gui *Gui) handleStopContainers() error {
	targets := gui.Panels.Containers.List.GetAllItems()

	return gui.createConfirmationPanel(gui.Tr.Confirm, gui.containersPrompt(targets, gui.Tr.ConfirmStopContainers), func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.StoppingStatus, func() error {
			for _, ctr := range targets {
				if err := ctr.Stop(); err != nil {
					gui.Log.Error(err)
				}
			}

			return nil
		})
	}, nil)
}

func (gui *Gui) handleRemoveContainers() error {
	targets := gui.Panels.Containers.List.GetAllItems()

	return gui.createConfirmationPanel(gui.Tr.Confirm, gui.containersPrompt(targets, gui.Tr.ConfirmRemoveContainers), func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.RemovingStatus, func() error {
			for _, ctr := range targets {
				if err := ctr.Remove(container.RemoveOptions{Force: true}); err != nil {
					gui.Log.Error(err)
				}
			}

			return nil
		})
	}, nil)
}

func (gui *Gui) handleContainersBulkCommand(g *gocui.Gui, v *gocui.View) error {
	baseBulkCommands := []config.CustomCommand{
		{
			Name:             gui.Tr.UpdateAllContainers,
			InternalFunction: gui.handleUpdateAllContainers,
		},
		{
			Name:             gui.Tr.StopAllContainers,
			InternalFunction: gui.handleStopContainers,
		},
		{
			Name:             gui.Tr.RemoveAllContainers,
			InternalFunction: gui.handleRemoveContainers,
		},
		{
			Name:             gui.Tr.PruneContainers,
			InternalFunction: gui.handlePruneContainers,
		},
	}

	bulkCommands := append(baseBulkCommands, gui.Config.UserConfig.BulkCommands.Containers...)
	commandObject := gui.DockerCommand.NewCommandObject(commands.CommandObject{})

	return gui.createBulkCommandMenu(bulkCommands, commandObject)
}

// Open first port in browser
func (gui *Gui) handleContainersOpenInBrowserCommand(g *gocui.Gui, v *gocui.View) error {
	ctr, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.openContainerInBrowser(ctr)
}

func (gui *Gui) openContainerInBrowser(ctr *commands.Container) error {
	// skip if no any ports
	if len(ctr.Container.Ports) == 0 {
		return nil
	}
	// skip if the first port is not published
	port := ctr.Container.Ports[0]
	if port.IP == "" {
		return nil
	}
	ip := port.IP
	if ip == "0.0.0.0" {
		ip = "localhost"
	}
	link := fmt.Sprintf("http://%s:%d/", ip, port.PublicPort)
	return gui.OSCommand.OpenLink(link)
}
