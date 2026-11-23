package commands

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/go-errors/errors"
	"github.com/mgutz/str"
)

// ComposeConfigStatus says whether a service's compose configuration still
// matches the one its container was created from.
type ComposeConfigStatus int

const (
	// ComposeConfigUnknown means the comparison could not be made.
	ComposeConfigUnknown ComposeConfigStatus = iota
	ComposeConfigUnchanged
	ComposeConfigChanged
)

// ComposeRecreateResult describes the container docker compose created in place
// of the one that was recreated.
type ComposeRecreateResult struct {
	OldName string
	// NewID and NewName are empty if the replacement could not be identified.
	NewID   string
	NewName string
}

// DockerName returns the name the docker daemon knows the container by.
func (c *Container) DockerName() string {
	if len(c.Container.Names) > 0 {
		return strings.TrimPrefix(c.Container.Names[0], "/")
	}
	return strings.TrimPrefix(c.Name, "/")
}

// MissingConfigFiles returns the compose files that cannot be read from this
// machine, for example because the project was started on another host.
func (info ComposeInfo) MissingConfigFiles() []string {
	var missing []string
	for _, file := range info.ConfigFiles {
		if _, err := os.Stat(file); err != nil {
			missing = append(missing, file)
		}
	}
	return missing
}

// composeBaseCommand turns the configured docker compose command into an argv,
// dropping the flags that select a project. Those describe the project
// lazydocker was launched in, which is not necessarily the one the container
// belongs to.
func composeBaseCommand(template string) []string {
	args := str.ToArgv(template)
	base := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-f", arg == "--file", arg == "-p", arg == "--project-name", arg == "--project-directory":
			i++
		case strings.HasPrefix(arg, "--file="), strings.HasPrefix(arg, "--project-name="), strings.HasPrefix(arg, "--project-directory="):
		default:
			base = append(base, arg)
		}
	}

	if len(base) == 0 {
		return []string{"docker", "compose"}
	}
	return base
}

// composeArgs builds a docker compose invocation that targets the project the
// container was created by, so it works no matter which directory lazydocker
// was launched from.
func composeArgs(template string, info ComposeInfo, args ...string) []string {
	argv := composeBaseCommand(template)
	argv = append(argv, "-p", info.Project)
	if info.WorkingDir != "" {
		argv = append(argv, "--project-directory", info.WorkingDir)
	}
	for _, file := range info.ConfigFiles {
		argv = append(argv, "-f", file)
	}
	for _, file := range info.EnvFiles {
		argv = append(argv, "--env-file", file)
	}
	return append(argv, args...)
}

// parseComposeConfigHash extracts the hash of the given service from the output
// of `docker compose config --hash <service>`, which prints "<service> <hash>".
func parseComposeConfigHash(output, service string) string {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == service {
			return fields[1]
		}
	}
	return ""
}

// runCompose runs docker compose against the container's project and returns
// its stdout.
func (c *Container) runCompose(info ComposeInfo, args ...string) (string, error) {
	argv := composeArgs(c.OSCommand.Config.UserConfig.CommandTemplates.DockerCompose, info, args...)
	c.Log.Warn("running " + strings.Join(argv, " "))

	cmd := c.OSCommand.NewCmd(argv[0], argv[1:]...)
	if info.WorkingDir != "" {
		if stat, err := os.Stat(info.WorkingDir); err == nil && stat.IsDir() {
			cmd.Dir = info.WorkingDir
		}
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		if message == "" {
			return "", err
		}
		return "", errors.New(message)
	}

	return stdout.String(), nil
}

// ComposeConfigStatus compares the service configuration in the compose files
// on disk against the configuration the container was created from.
func (c *Container) ComposeConfigStatus() ComposeConfigStatus {
	info, ok := c.ComposeInfo()
	if !ok || info.ConfigHash == "" || info.Service == "" {
		return ComposeConfigUnknown
	}

	output, err := c.runCompose(info, "config", "--hash", info.Service)
	if err != nil {
		c.Log.Warn(err)
		return ComposeConfigUnknown
	}

	switch parseComposeConfigHash(output, info.Service) {
	case "":
		return ComposeConfigUnknown
	case info.ConfigHash:
		return ComposeConfigUnchanged
	default:
		return ComposeConfigChanged
	}
}

// RecreateFromCompose recreates the container through docker compose, so that
// it picks up the current contents of its compose files. Unlike Recreate, which
// clones the existing container, the compose files decide everything about the
// new container, including its name.
func (c *Container) RecreateFromCompose(ctx context.Context) (ComposeRecreateResult, error) {
	result := ComposeRecreateResult{OldName: c.DockerName()}

	info, ok := c.ComposeInfo()
	if !ok || info.Service == "" {
		return result, errors.New(c.Tr.NotComposeManaged)
	}
	if len(info.ConfigFiles) == 0 {
		return result, errors.New(c.Tr.ComposeFilesUnknown)
	}
	if missing := info.MissingConfigFiles(); len(missing) > 0 {
		return result, errors.New(fmt.Sprintf("%s\n%s", c.Tr.ComposeFilesNotFound, strings.Join(missing, "\n")))
	}

	if _, err := c.runCompose(info, "up", "-d", "--no-deps", "--force-recreate", info.Service); err != nil {
		return result, err
	}

	result.NewID, result.NewName = c.findComposeReplacement(ctx, info)
	return result, nil
}

// findComposeReplacement looks up the container that now fills this container's
// slot in its compose service.
func (c *Container) findComposeReplacement(ctx context.Context, info ComposeInfo) (string, string) {
	args := filters.NewArgs(
		filters.Arg("label", "com.docker.compose.project="+info.Project),
		filters.Arg("label", "com.docker.compose.service="+info.Service),
		filters.Arg("label", "com.docker.compose.oneoff=False"),
	)
	if info.ContainerNumber != "" {
		args.Add("label", "com.docker.compose.container-number="+info.ContainerNumber)
	}

	containers, err := c.Client.ContainerList(ctx, container.ListOptions{All: true, Filters: args})
	if err != nil {
		c.Log.Warn(err)
		return "", ""
	}
	if len(containers) != 1 || len(containers[0].Names) == 0 {
		return "", ""
	}

	return containers[0].ID, strings.TrimPrefix(containers[0].Names[0], "/")
}
