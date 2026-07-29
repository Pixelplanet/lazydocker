package commands

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	dockerimage "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/go-errors/errors"
	"github.com/jesseduffield/lazydocker/pkg/i18n"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/sasha-s/go-deadlock"
	"github.com/sirupsen/logrus"
	"golang.org/x/xerrors"
)

// Container : A docker Container
type Container struct {
	Name            string
	ServiceName     string
	ContainerNumber string // might make this an int in the future if need be

	// OneOff tells us if the container is just a job container or is actually bound to the service
	OneOff          bool
	ProjectName     string
	ID              string
	Container       container.Summary
	Client          *client.Client
	OSCommand       *OSCommand
	Log             *logrus.Entry
	StatHistory     []*RecordedStats
	Details         container.InspectResponse
	MonitoringStats bool
	DockerCommand   LimitedDockerCommand
	Tr              *i18n.TranslationSet

	StatsMutex deadlock.Mutex
}

// Remove removes the container
func (c *Container) Remove(options container.RemoveOptions) error {
	c.Log.Warn(fmt.Sprintf("removing container %s", c.Name))
	if err := c.Client.ContainerRemove(context.Background(), c.ID, options); err != nil {
		if strings.Contains(err.Error(), "Stop the container before attempting removal or force remove") {
			return ComplexError{
				Code:    MustStopContainer,
				Message: err.Error(),
				frame:   xerrors.Caller(1),
			}
		}
		return err
	}

	return nil
}

// Start starts the container
func (c *Container) Start() error {
	c.Log.Warn(fmt.Sprintf("starting container %s", c.Name))
	return c.Client.ContainerStart(context.Background(), c.ID, container.StartOptions{})
}

// Stop stops the container
func (c *Container) Stop() error {
	c.Log.Warn(fmt.Sprintf("stopping container %s", c.Name))
	return c.Client.ContainerStop(context.Background(), c.ID, container.StopOptions{})
}

// Pause pauses the container
func (c *Container) Pause() error {
	c.Log.Warn(fmt.Sprintf("pausing container %s", c.Name))
	return c.Client.ContainerPause(context.Background(), c.ID)
}

// Unpause unpauses the container
func (c *Container) Unpause() error {
	c.Log.Warn(fmt.Sprintf("unpausing container %s", c.Name))
	return c.Client.ContainerUnpause(context.Background(), c.ID)
}

// Restart restarts the container
func (c *Container) Restart() error {
	c.Log.Warn(fmt.Sprintf("restarting container %s", c.Name))
	return c.Client.ContainerRestart(context.Background(), c.ID, container.StopOptions{})
}

// Rename renames the container to the given name.
func (c *Container) Rename(name string) error {
	c.Log.Warn(fmt.Sprintf("renaming container %s to %s", c.Name, name))
	return c.Client.ContainerRename(context.Background(), c.ID, name)
}

// EnvVars returns the container's environment variables as an ordered slice of
// "KEY=VALUE" strings, or nil if the details have not been loaded yet.
func (c *Container) EnvVars() []string {
	if !c.DetailsLoaded() || c.Details.Config == nil {
		return nil
	}
	return c.Details.Config.Env
}

// UpdateRestartPolicy updates the restart policy of the container in place
// (without recreating it).
func (c *Container) UpdateRestartPolicy(policy container.RestartPolicyMode) error {
	c.Log.Warn(fmt.Sprintf("updating restart policy of container %s to %s", c.Name, policy))
	_, err := c.Client.ContainerUpdate(context.Background(), c.ID, container.UpdateConfig{
		RestartPolicy: container.RestartPolicy{Name: policy},
	})
	return err
}

// RecreateOptions configures how a container is recreated.
type RecreateOptions struct {
	// PullImage, when true, pulls the latest version of the container's image
	// before recreating it.
	PullImage bool
	// NewEnv, when non-nil, replaces the container's environment variables.
	// A nil value preserves the existing environment.
	NewEnv []string
}

// Recreate stops and removes the container, then creates and starts a new
// container that preserves the original configuration (environment variables,
// volumes, ports, networks, labels, restart policy, command, etc.).
//
// When opts.PullImage is true the container's image is pulled first so the new
// container runs the latest available image.
func (c *Container) Recreate(ctx context.Context, opts RecreateOptions) error {
	details, err := c.Inspect()
	if err != nil {
		return err
	}

	if details.Config == nil || details.ContainerJSONBase == nil {
		return errors.New(c.Tr.WaitingForContainerInfo)
	}

	config := details.Config
	hostConfig := details.HostConfig

	if opts.NewEnv != nil {
		config.Env = opts.NewEnv
	}

	imageName := config.Image

	if opts.PullImage {
		c.Log.Warn(fmt.Sprintf("pulling image %s", imageName))
		reader, err := c.Client.ImagePull(ctx, imageName, dockerimage.PullOptions{})
		if err != nil {
			return err
		}
		// We must drain the reader for the pull to complete.
		_, _ = io.Copy(io.Discard, reader)
		reader.Close()
	}

	networkingConfig := buildNetworkingConfig(details, config)

	name := strings.TrimPrefix(details.Name, "/")

	if details.State != nil && details.State.Running {
		c.Log.Warn(fmt.Sprintf("stopping container %s before recreation", name))
		if err := c.Client.ContainerStop(ctx, c.ID, container.StopOptions{}); err != nil {
			return err
		}
	}

	// The old container must be removed so its name can be reused.
	c.Log.Warn(fmt.Sprintf("removing old container %s", name))
	if err := c.Client.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
		return err
	}

	c.Log.Warn(fmt.Sprintf("creating new container %s", name))
	created, err := c.Client.ContainerCreate(ctx, config, hostConfig, networkingConfig, nil, name)
	if err != nil {
		return err
	}

	return c.Client.ContainerStart(ctx, created.ID, container.StartOptions{})
}

// buildNetworkingConfig reconstructs a NetworkingConfig from the inspected
// container, stripping runtime-only fields that would conflict when creating a
// new container (such as the container's own generated aliases and assigned
// addresses).
func buildNetworkingConfig(details container.InspectResponse, config *container.Config) *network.NetworkingConfig {
	if details.NetworkSettings == nil || len(details.NetworkSettings.Networks) == 0 {
		return &network.NetworkingConfig{}
	}

	shortID := details.ID
	if len(shortID) > 12 {
		shortID = shortID[:12]
	}

	endpoints := make(map[string]*network.EndpointSettings, len(details.NetworkSettings.Networks))
	for name, endpoint := range details.NetworkSettings.Networks {
		if endpoint == nil {
			continue
		}

		cleaned := &network.EndpointSettings{
			IPAMConfig: endpoint.IPAMConfig,
			Links:      endpoint.Links,
			NetworkID:  endpoint.NetworkID,
			Aliases:    filterAliases(endpoint.Aliases, shortID, config.Hostname),
		}
		endpoints[name] = cleaned
	}

	return &network.NetworkingConfig{EndpointsConfig: endpoints}
}

// filterAliases removes auto-generated aliases (the container's short ID and
// hostname) which Docker rejects when supplied explicitly on create.
func filterAliases(aliases []string, shortID, hostname string) []string {
	if len(aliases) == 0 {
		return nil
	}

	filtered := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		if alias == shortID || alias == hostname {
			continue
		}
		filtered = append(filtered, alias)
	}

	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

// Attach attaches the container
func (c *Container) Attach() (*exec.Cmd, error) {
	if !c.DetailsLoaded() {
		return nil, errors.New(c.Tr.WaitingForContainerInfo)
	}

	// verify that we can in fact attach to this container
	if !c.Details.Config.OpenStdin {
		return nil, errors.New(c.Tr.UnattachableContainerError)
	}

	if c.Container.State == "exited" {
		return nil, errors.New(c.Tr.CannotAttachStoppedContainerError)
	}

	c.Log.Warn(fmt.Sprintf("attaching to container %s", c.Name))
	// TODO: use SDK
	cmd := c.OSCommand.NewCmd("docker", "attach", "--sig-proxy=false", c.ID)
	return cmd, nil
}

// Top returns process information
func (c *Container) Top(ctx context.Context) (container.TopResponse, error) {
	detail, err := c.Inspect()
	if err != nil {
		return container.TopResponse{}, err
	}

	// check container status
	if !detail.State.Running {
		return container.TopResponse{}, errors.New("container is not running")
	}

	return c.Client.ContainerTop(ctx, c.ID, []string{})
}

// PruneContainers prunes containers
func (c *DockerCommand) PruneContainers() error {
	_, err := c.Client.ContainersPrune(context.Background(), filters.Args{})
	return err
}

// Inspect returns details about the container
func (c *Container) Inspect() (container.InspectResponse, error) {
	return c.Client.ContainerInspect(context.Background(), c.ID)
}

// RenderTop returns details about the container
func (c *Container) RenderTop(ctx context.Context) (string, error) {
	result, err := c.Top(ctx)
	if err != nil {
		return "", err
	}

	return utils.RenderTable(append([][]string{result.Titles}, result.Processes...))
}

// DetailsLoaded tells us whether we have yet loaded the details for a container.
// Sometimes it takes some time for a container to have its details loaded
// after it starts.
func (c *Container) DetailsLoaded() bool {
	return c.Details.ContainerJSONBase != nil
}
