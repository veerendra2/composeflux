package dockercompose

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/distribution/reference"
	dockerconfigtypes "github.com/docker/cli/cli/config/types"
	dockerregistry "github.com/docker/docker/registry"
	mobyClient "github.com/moby/moby/client"
)

// HasImageUpdates checks if any service image in the project has a newer version in the registry.
func (c *client) HasImageUpdates(ctx context.Context, project *types.Project) (bool, error) {
	for _, svc := range project.Services {
		if svc.Build != nil || svc.Image == "" {
			continue
		}

		named, parseErr := reference.ParseNormalizedNamed(svc.Image)
		if _, isDigested := named.(reference.Digested); parseErr == nil && isDigested {
			continue
		}

		localInfo, err := c.docker.ImageInspect(ctx, svc.Image)
		if err != nil {
			type notFound interface{ NotFound() }
			if _, ok := err.(notFound); !ok {
				slog.Warn("Failed to inspect image, skipping", "stack", project.Name, "service", svc.Name, "image", svc.Image, "error", err)
				continue
			}
			// Image not present locally — treat as needs update; compose up will pull it
			slog.Debug("Image not found locally, treating as update needed",
				"stack", project.Name, "service", svc.Name, "image", svc.Image)
			return true, nil
		}

		if len(localInfo.RepoDigests) == 0 {
			// No repo digests means the image was built or loaded locally — skip
			continue
		}

		encodedAuth := ""
		if parseErr == nil {
			encodedAuth = c.registryAuth(named)
		}

		remoteDist, err := c.docker.DistributionInspect(ctx, svc.Image, mobyClient.DistributionInspectOptions{
			EncodedRegistryAuth: encodedAuth,
		})
		if err != nil {
			slog.Warn("Failed to fetch remote manifest, skipping service", "image", svc.Image, "error", err)
			continue
		}

		remoteDigest := remoteDist.Descriptor.Digest.String()
		if !containsDigest(localInfo.RepoDigests, remoteDigest) {
			slog.Info("Image update available", "stack", project.Name, "service", svc.Name, "image", svc.Image)
			slog.Debug("Image digest mismatch", "image", svc.Image,
				"local_digests", localInfo.RepoDigests, "remote_digest", remoteDigest)
			return true, nil
		}
	}
	return false, nil
}

// registryAuth encodes Docker CLI credentials for a registry manifest request.
func (c *client) registryAuth(named reference.Named) string {
	repoInfo, err := dockerregistry.ParseRepositoryInfo(named)
	if err != nil {
		return ""
	}
	auth, _ := c.dockerCLI.ConfigFile().GetAuthConfig(repoInfo.Index.Name)
	encoded, err := json.Marshal(dockerconfigtypes.AuthConfig(auth))
	if err != nil {
		return ""
	}
	return base64.URLEncoding.EncodeToString(encoded)
}

// containsDigest reports whether any local repository digest matches the remote manifest.
func containsDigest(repoDigests []string, remoteDigest string) bool {
	for _, repoDigest := range repoDigests {
		parts := strings.SplitN(repoDigest, "@", 2)
		if len(parts) == 2 && parts[1] == remoteDigest {
			return true
		}
	}
	return false
}
