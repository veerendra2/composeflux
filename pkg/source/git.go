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
	DeployKeySecretRef string `name:"deploy-key-secret-ref" help:"Deploy key secret reference (name or ID) to fetch from the remote secrets provider (leave empty to use existing key at ssh-key-path)" env:"GIT_DEPLOY_KEY_SECRET_REF" default:"" group:"Git Source Options:"`
	ClonePath          string `name:"clone-path" help:"Local directory for git clone" env:"GIT_CLONE_PATH" default:"/opt/compose-stack"`
	Branch             string `name:"branch" help:"Git branch to track" env:"GIT_BRANCH" default:"main" group:"Git Source Options:"`
}

type Client interface {
	Pull(ctx context.Context) ([]string, error)
	HasUpdates(ctx context.Context) (bool, string, string, error)
	GetChangedFiles(ctx context.Context, oldSHA, newSHA string) ([]string, error)
	Path() string
}

type client struct {
	repo    *git.Repository
	branch  string
	path    string
	sshAuth *ssh.PublicKeys
}

// Pull syncs latest changes from remote and returns a list of changed relative file paths since previous HEAD.
func (c *client) Pull(ctx context.Context) ([]string, error) {
	// Capture local HEAD before fetch to compute the diff. In daemon mode,
	// HasUpdates() advances the remote ref but leaves local HEAD at the old
	// commit, so this correctly diffs old..new. If local HEAD diverged
	// (manual checkout, crash), the diff may be misleading but the hard
	// reset below corrects the state.
	oldSHA := c.headSHA()
	err := c.fetch(ctx)
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil, fmt.Errorf("failed to fetch: %w", err)
	}

	remoteRef, err := c.remoteReference()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve remote ref: %w", err)
	}

	newSHA := remoteRef.Hash().String()

	var changedFiles []string
	if oldSHA != "" && oldSHA != newSHA {
		changedFiles, err = c.GetChangedFiles(ctx, oldSHA, newSHA)
		if err != nil {
			return nil, fmt.Errorf("failed to compute git diff file list (%s..%s): %w", shortSHA(oldSHA), shortSHA(newSHA), err)
		}
	}

	w, err := c.repo.Worktree()
	if err != nil {
		return nil, err
	}

	if err := w.Reset(&git.ResetOptions{Commit: remoteRef.Hash(), Mode: git.HardReset}); err != nil {
		return nil, fmt.Errorf("failed to reset to %s/%s: %w", remoteName, c.branch, err)
	}

	return changedFiles, nil
}

// GetChangedFiles compares two commit SHAs and returns relative paths of modified, added, or deleted files.
func (c *client) GetChangedFiles(ctx context.Context, oldSHA, newSHA string) ([]string, error) {
	if oldSHA == "" || newSHA == "" || oldSHA == newSHA {
		return nil, nil
	}

	oldCommit, err := c.repo.CommitObject(plumbing.NewHash(oldSHA))
	if err != nil {
		return nil, fmt.Errorf("failed to get old commit %s: %w", oldSHA, err)
	}

	newCommit, err := c.repo.CommitObject(plumbing.NewHash(newSHA))
	if err != nil {
		return nil, fmt.Errorf("failed to get new commit %s: %w", newSHA, err)
	}

	oldTree, err := oldCommit.Tree()
	if err != nil {
		return nil, fmt.Errorf("failed to get old tree: %w", err)
	}

	newTree, err := newCommit.Tree()
	if err != nil {
		return nil, fmt.Errorf("failed to get new tree: %w", err)
	}

	changes, err := oldTree.DiffContext(ctx, newTree)
	if err != nil {
		return nil, fmt.Errorf("failed to diff trees: %w", err)
	}

	var filePaths []string
	pathMap := make(map[string]struct{})

	for _, change := range changes {
		if change.From.Name != "" {
			pathMap[change.From.Name] = struct{}{}
		}
		if change.To.Name != "" {
			pathMap[change.To.Name] = struct{}{}
		}
	}

	for path := range pathMap {
		filePaths = append(filePaths, path)
	}

	return filePaths, nil
}

// HasUpdates checks for remote changes and returns update status with short commit SHAs (for logging)
func (c *client) HasUpdates(ctx context.Context) (bool, string, string, error) {
	localSHA := c.headSHA()
	err := c.fetch(ctx)

	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		return false, shortSHA(localSHA), shortSHA(localSHA), nil
	}

	if err != nil {
		return false, shortSHA(localSHA), "", err
	}

	remoteRef, err := c.remoteReference()
	remoteSHA := ""
	if err == nil {
		remoteSHA = remoteRef.Hash().String()
	}

	hasUpdates := localSHA != remoteSHA
	return hasUpdates, shortSHA(remoteSHA), shortSHA(localSHA), nil
}

// fetch force-updates the configured remote-tracking references.
func (c *client) fetch(ctx context.Context) error {
	return c.repo.FetchContext(ctx, &git.FetchOptions{
		RemoteName: remoteName,
		Auth:       c.sshAuth,
		Force:      true,
	})
}

// headSHA returns the current local HEAD hash or an empty string when unavailable.
func (c *client) headSHA() string {
	ref, err := c.repo.Head()
	if err != nil {
		return ""
	}
	return ref.Hash().String()
}

// remoteReference resolves the configured branch's origin tracking reference.
func (c *client) remoteReference() (*plumbing.Reference, error) {
	return c.repo.Reference(plumbing.NewRemoteReferenceName(remoteName, c.branch), true)
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

// New opens or clones the configured repository and prepares its tracked branch.
func New(cfg Config) (Client, error) {
	sshAuth, err := ssh.NewPublicKeysFromFile(sshUser, cfg.SSHKeyPath, "")
	if err != nil {
		return nil, fmt.Errorf("failed to load SSH key: %w", err)
	}

	if err := os.MkdirAll(cfg.ClonePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create clone directory: %w", err)
	}

	repo, cloned, err := openRepository(cfg, sshAuth)
	if err != nil {
		return nil, err
	}
	if !cloned {
		slog.Info("Opened existing repository", "url", cfg.RepoURL, "branch", cfg.Branch, "path", cfg.ClonePath)
		if err := checkoutBranch(repo, cfg.Branch, sshAuth); err != nil {
			return nil, err
		}
	}

	return &client{
		repo:    repo,
		branch:  cfg.Branch,
		path:    cfg.ClonePath,
		sshAuth: sshAuth,
	}, nil
}

// openRepository opens the existing clone or creates it from the configured remote.
func openRepository(cfg Config, auth *ssh.PublicKeys) (*git.Repository, bool, error) {
	repo, err := git.PlainOpen(cfg.ClonePath)
	if err == nil {
		return repo, false, nil
	}
	if !errors.Is(err, git.ErrRepositoryNotExists) {
		return nil, false, fmt.Errorf("failed to open repository: %w", err)
	}

	slog.Info("Cloning repository", "url", cfg.RepoURL, "branch", cfg.Branch, "path", cfg.ClonePath)
	repo, err = git.PlainClone(cfg.ClonePath, false, &git.CloneOptions{
		URL:           cfg.RepoURL,
		Auth:          auth,
		ReferenceName: plumbing.NewBranchReferenceName(cfg.Branch),
	})
	if err != nil {
		return nil, false, fmt.Errorf("failed to clone: %w", err)
	}
	return repo, true, nil
}

// checkoutBranch checks out an existing local branch or creates it from origin.
func checkoutBranch(repo *git.Repository, branch string, auth *ssh.PublicKeys) error {
	branchRef := plumbing.NewBranchReferenceName(branch)
	worktree, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	options := &git.CheckoutOptions{Branch: branchRef}
	if _, err := repo.Reference(branchRef, false); err != nil {
		if err := repo.Fetch(&git.FetchOptions{RemoteName: remoteName, Auth: auth, Force: true}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
			return fmt.Errorf("failed to fetch: %w", err)
		}
		remoteRef, err := repo.Reference(plumbing.NewRemoteReferenceName(remoteName, branch), true)
		if err != nil {
			return fmt.Errorf("branch %q not found on remote: %w", branch, err)
		}
		options.Hash = remoteRef.Hash()
		options.Create = true
	}
	if err := worktree.Checkout(options); err != nil {
		return fmt.Errorf("failed to checkout branch %q: %w", branch, err)
	}
	return nil
}
