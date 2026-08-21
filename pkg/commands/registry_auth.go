package commands

import (
	"io"

	"github.com/distribution/reference"
	cliconfig "github.com/docker/cli/cli/config"
	"github.com/docker/docker/api/types/registry"
)

// dockerHubIndexServer is the credential-store key that `docker login` uses for
// Docker Hub credentials in ~/.docker/config.json.
const dockerHubIndexServer = "https://index.docker.io/v1/"

// resolveRegistryAuth returns the base64url-encoded registry credentials for the
// given image reference, loaded from the docker CLI config / credential store
// (whatever `docker login` saved). It returns an empty string when no
// credentials are configured, in which case the pull proceeds anonymously.
//
// The Docker SDK's ImagePull does not read ~/.docker/config.json on its own, so
// without this the daemon would reject pulls of private images even after the
// user has run `docker login`.
func resolveRegistryAuth(imageRef string) string {
	cf := cliconfig.LoadDefaultConfigFile(io.Discard)

	authConfig, err := cf.GetAuthConfig(registryHost(imageRef))
	if err != nil {
		return ""
	}

	if authConfig.Username == "" && authConfig.Password == "" &&
		authConfig.IdentityToken == "" && authConfig.RegistryToken == "" {
		return ""
	}

	encoded, err := registry.EncodeAuthConfig(registry.AuthConfig{
		Username:      authConfig.Username,
		Password:      authConfig.Password,
		Auth:          authConfig.Auth,
		Email:         authConfig.Email,
		ServerAddress: authConfig.ServerAddress,
		IdentityToken: authConfig.IdentityToken,
		RegistryToken: authConfig.RegistryToken,
	})
	if err != nil {
		return ""
	}
	return encoded
}

// registryHost returns the credential-store key for the given image reference.
// Docker Hub images map to the special index-server key that `docker login`
// uses by default.
func registryHost(imageRef string) string {
	named, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		return dockerHubIndexServer
	}
	if domain := reference.Domain(named); domain != "docker.io" {
		return domain
	}
	return dockerHubIndexServer
}
