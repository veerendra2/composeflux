package source

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

const (
	SSHUser    = "git"
	RemoteName = "origin"
	Branch     = "main"
)

type Config struct {
	RepoURL            string `name:"repo-url" help:"Git repository URL (SSH)" env:"GIT_REPO_URL" required:""`
	SSHKeyPath         string `name:"ssh-key-path" help:"Path to SSH private key" env:"GIT_SSH_KEY_PATH" default:"/.ssh/composeflux_id_rsa"`
	DeployKeySecretRef string `name:"deploy-key-secret-ref" help:"Deploy key secret reference (name or ID) to fetch from secrets manager (leave empty to use existing key at ssh-key-path)" env:"GIT_DEPLOY_KEY_SECRET_REF" default:"SSH_PRIVATE_KEY" group:"Git Source Options:"`
	ClonePath          string `name:"clone-path" help:"Local directory for git clone" env:"GIT_CLONE_PATH" default:"/opt/compose-stack"`
}

type Client interface {
	Pull(ctx context.Context) error
	HasUpdates(ctx context.Context) (bool, string, string, error)
	Path() string
}

type client struct {
	repo    *git.Repository
	branch  string
	path    string
	sshAuth *ssh.PublicKeys
}

// Pull syncs latest changes from remote
func (c *client) Pull(ctx context.Context) error {
	w, err := c.repo.Worktree()
	if err != nil {
		return err
	}
	err = w.PullContext(ctx, &git.PullOptions{
		RemoteName: RemoteName,
		Auth:       c.sshAuth,
	})

	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return err
	}

	return nil
}

// HasUpdates checks for remote changes and returns update status with commit SHAs
func (c *client) HasUpdates(ctx context.Context) (bool, string, string, error) {
	// Get local HEAD commit
	localRef, err := c.repo.Head()
	localSHA := ""
	if err == nil {
		localSHA = localRef.Hash().String()[:7]
	}

	// Fetch from remote
	err = c.repo.FetchContext(ctx, &git.FetchOptions{
		RemoteName: RemoteName,
		Auth:       c.sshAuth,
	})

	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		return false, localSHA, localSHA, nil
	}

	if err != nil {
		return false, localSHA, "", err
	}

	// Get remote HEAD commit
	remoteBranchRef := plumbing.ReferenceName(fmt.Sprintf("refs/remotes/%s/%s", RemoteName, c.branch))
	remoteRef, err := c.repo.Reference(remoteBranchRef, true)
	remoteSHA := ""
	if err == nil {
		remoteSHA = remoteRef.Hash().String()[:7]
	}

	hasUpdates := localSHA != remoteSHA
	return hasUpdates, remoteSHA, localSHA, nil
}

// Path returns the local repository path
func (c *client) Path() string {
	return c.path
}

func New(cfg Config) (Client, error) {
	sshAuth, err := ssh.NewPublicKeysFromFile(SSHUser, cfg.SSHKeyPath, "")
	if err != nil {
		return nil, fmt.Errorf("failed to load SSH key: %w", err)
	}

	if err := os.MkdirAll(cfg.ClonePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create clone directory: %w", err)
	}

	repo, err := git.PlainOpen(cfg.ClonePath)
	if err != nil {
		if errors.Is(err, git.ErrRepositoryNotExists) {
			slog.Info("Cloning repository", "url", cfg.RepoURL, "path", cfg.ClonePath)
			repo, err = git.PlainClone(cfg.ClonePath, false, &git.CloneOptions{
				URL:  cfg.RepoURL,
				Auth: sshAuth,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to clone: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to open repository: %w", err)
		}
	}

	return &client{
		repo:    repo,
		branch:  Branch,
		path:    cfg.ClonePath,
		sshAuth: sshAuth,
	}, nil
}
