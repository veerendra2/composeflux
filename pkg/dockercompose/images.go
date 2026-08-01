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
	mobyClient "github.com/moby/moby/client"
	dockerregistry "github.com/docker/docker/registry"
)

// HasImageUpdates checks if any service image in the project has a newer version in the registry.
func (c *client) HasImageUpdates(ctx context.Context, project *types.Project) (bool, error) {
	for _, svc := range project.Services {
		if svc.Build != nil || svc.Image == "" {
			continue
		}

		// Skip digest-pinned images (e.g. image@sha256:abc…) — they are immutable
		named, parseErr := reference.ParseNormalizedNamed(svc.Image)
		if parseErr == nil {
			if _, isDigested := named.(reference.Digested); isDigested {
				continue
			}
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

		// Build auth token from docker config for private registry support
		encodedAuth := ""
		if parseErr == nil {
			if repoInfo, repoErr := dockerregistry.ParseRepositoryInfo(named); repoErr == nil {
				cliAuth, _ := c.dockerCLI.ConfigFile().GetAuthConfig(repoInfo.Index.Name)
				if buf, err := json.Marshal(dockerconfigtypes.AuthConfig(cliAuth)); err == nil {
					encodedAuth = base64.URLEncoding.EncodeToString(buf)
				}
			}
		}

		remoteDist, err := c.docker.DistributionInspect(ctx, svc.Image, mobyClient.DistributionInspectOptions{
			EncodedRegistryAuth: encodedAuth,
		})
		if err != nil {
			slog.Warn("Failed to fetch remote manifest, skipping service", "image", svc.Image, "error", err)
			continue
		}

		remoteDigest := remoteDist.Descriptor.Digest.String()
		hasMatch := false
		for _, localDigest := range localInfo.RepoDigests {
			// localDigest format: "name@sha256:abc…" — compare only the digest part
			parts := strings.SplitN(localDigest, "@", 2)
			if len(parts) == 2 && parts[1] == remoteDigest {
				hasMatch = true
				break
			}
		}

		if !hasMatch {
			slog.Info("Image update available", "stack", project.Name, "service", svc.Name, "image", svc.Image)
			slog.Debug("Image digest mismatch", "image", svc.Image,
				"local_digests", localInfo.RepoDigests, "remote_digest", remoteDigest)
			return true, nil
		}
	}
	return false, nil
}
