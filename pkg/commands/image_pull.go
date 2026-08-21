package commands

import (
	"strings"

	"github.com/distribution/reference"
)

func pullImage(osCommand *OSCommand, imageName string) error {
	err := osCommand.RunCommand("docker pull " + osCommand.Quote(imageName))
	if err == nil || !isLocalImageReference(imageName) {
		return err
	}

	if osCommand.RunCommand("docker image inspect " + osCommand.Quote(imageName)) == nil {
		return nil
	}
	return err
}

func isLocalImageReference(imageName string) bool {
	named, err := reference.ParseNormalizedNamed(imageName)
	if err != nil {
		return false
	}
	return !strings.Contains(reference.FamiliarName(named), "/")
}
