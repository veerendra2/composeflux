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
	sshUser    = "git"
	remoteName = "origin"
)

type Config struct {
	RepoURL            string `name:"repo-url" help:"Git repository URL (SSH)" env:"GIT_REPO_URL" required:""`
	SSHKeyPath         string `name:"ssh-key-path" help:"Path to SSH private key" env:"GIT_SSH_KEY_PATH" default:"/.ssh/composeflux_id_rsa"`
	DeployKeySecretRef string `name:"deploy-key-secret-ref" help:"Deploy key secret reference (name or ID) to fetch from secrets manager (leave empty to use existing key at ssh-key-path)" env:"GIT_DEPLOY_KEY_SECRET_REF" default:"" group:"Git Source Options:"`
	ClonePath          string `name:"clone-path" help:"Local directory for git clone" env:"GIT_CLONE_PATH" default:"/opt/compose-stack"`
	Branch             string `name:"branch" help:"Git branch to track" env:"GIT_BRANCH" default:"main" group:"Git Source Options:"`
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
	err := c.repo.FetchContext(ctx, &git.FetchOptions{
		RemoteName: remoteName,
		Auth:       c.sshAuth,
		Force:      true, // required for force-pushed branches to update remote tracking refs
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("failed to fetch: %w", err)
	}

	// Hard reset to remote tracking ref — handles force-push and cases where
	// HasUpdates() already fetched (making PullContext return NoErrAlreadyUpToDate early
	// without actually updating the worktree)
	remoteRef, err := c.repo.Reference(plumbing.NewRemoteReferenceName(remoteName, c.branch), true)
	if err != nil {
		return fmt.Errorf("failed to resolve remote ref: %w", err)
	}

	w, err := c.repo.Worktree()
	if err != nil {
		return err
	}

	if err := w.Reset(&git.ResetOptions{Commit: remoteRef.Hash(), Mode: git.HardReset}); err != nil {
		return fmt.Errorf("failed to reset to %s/%s: %w", remoteName, c.branch, err)
	}
	return nil
}

// HasUpdates checks for remote changes and returns update status with short commit SHAs (for logging)
func (c *client) HasUpdates(ctx context.Context) (bool, string, string, error) {
	// Get local HEAD commit
	localRef, err := c.repo.Head()
	localSHA := ""
	if err == nil {
		localSHA = localRef.Hash().String()
	}

	// Fetch from remote
	err = c.repo.FetchContext(ctx, &git.FetchOptions{
		RemoteName: remoteName,
		Auth:       c.sshAuth,
		Force:      true,
	})

	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		return false, shortSHA(localSHA), shortSHA(localSHA), nil
	}

	if err != nil {
		return false, shortSHA(localSHA), "", err
	}

	// Get remote HEAD commit
	remoteBranchRef := plumbing.ReferenceName(fmt.Sprintf("refs/remotes/%s/%s", remoteName, c.branch))
	remoteRef, err := c.repo.Reference(remoteBranchRef, true)
	remoteSHA := ""
	if err == nil {
		remoteSHA = remoteRef.Hash().String()
	}

	hasUpdates := localSHA != remoteSHA
	return hasUpdates, shortSHA(remoteSHA), shortSHA(localSHA), nil
}

// shortSHA returns the first 7 characters of a SHA for display purposes.
func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// Path returns the local repository path
func (c *client) Path() string {
	return c.path
}

func New(cfg Config) (Client, error) {
	sshAuth, err := ssh.NewPublicKeysFromFile(sshUser, cfg.SSHKeyPath, "")
	if err != nil {
		return nil, fmt.Errorf("failed to load SSH key: %w", err)
	}

	if err := os.MkdirAll(cfg.ClonePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create clone directory: %w", err)
	}

	repo, err := git.PlainOpen(cfg.ClonePath)
	if err != nil {
		if errors.Is(err, git.ErrRepositoryNotExists) {
			slog.Info("Cloning repository", "url", cfg.RepoURL, "branch", cfg.Branch)
			repo, err = git.PlainClone(cfg.ClonePath, false, &git.CloneOptions{
				URL:           cfg.RepoURL,
				Auth:          sshAuth,
				ReferenceName: plumbing.NewBranchReferenceName(cfg.Branch),
			})
			if err != nil {
				return nil, fmt.Errorf("failed to clone: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to open repository: %w", err)
		}
	} else {
		slog.Info("Opened existing repository", "url", cfg.RepoURL, "branch", cfg.Branch)
		branchRef := plumbing.NewBranchReferenceName(cfg.Branch)
		w, err := repo.Worktree()
		if err != nil {
			return nil, fmt.Errorf("failed to get worktree: %w", err)
		}

		opts := &git.CheckoutOptions{Branch: branchRef}
		if _, err := repo.Reference(branchRef, false); err != nil {
			// Local branch doesn't exist yet — fetch and create it from remote tracking ref
			if err := repo.Fetch(&git.FetchOptions{RemoteName: remoteName, Auth: sshAuth, Force: true}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
				return nil, fmt.Errorf("failed to fetch: %w", err)
			}
			remoteRef, err := repo.Reference(plumbing.NewRemoteReferenceName(remoteName, cfg.Branch), true)
			if err != nil {
				return nil, fmt.Errorf("branch %q not found on remote: %w", cfg.Branch, err)
			}
			opts.Hash = remoteRef.Hash()
			opts.Create = true
		}

		if err := w.Checkout(opts); err != nil {
			return nil, fmt.Errorf("failed to checkout branch %q: %w", cfg.Branch, err)
		}
	}

	return &client{
		repo:    repo,
		branch:  cfg.Branch,
		path:    cfg.ClonePath,
		sshAuth: sshAuth,
	}, nil
}
