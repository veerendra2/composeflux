package gitrepo

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/crypto/ssh"
)

// upstream is a local repository used as the remote; the file transport ignores SSH auth.
type upstream struct {
	dir  string
	repo *git.Repository
}

func newUpstream() *upstream {
	dir := GinkgoT().TempDir()
	repo, err := git.PlainInitWithOptions(dir, &git.PlainInitOptions{
		InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
	})
	Expect(err).NotTo(HaveOccurred())
	return &upstream{dir: dir, repo: repo}
}

// commit writes files (nil content deletes) and commits them on the current branch.
func (u *upstream) commit(files map[string]*string) string {
	w, err := u.repo.Worktree()
	Expect(err).NotTo(HaveOccurred())
	for name, content := range files {
		path := filepath.Join(u.dir, name)
		if content == nil {
			_, err := w.Remove(name)
			Expect(err).NotTo(HaveOccurred())
			continue
		}
		Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
		Expect(os.WriteFile(path, []byte(*content), 0o644)).To(Succeed())
		_, err := w.Add(name)
		Expect(err).NotTo(HaveOccurred())
	}
	hash, err := w.Commit("test commit", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@example.com", When: time.Now()},
	})
	Expect(err).NotTo(HaveOccurred())
	return hash.String()
}

func content(s string) *string { return &s }

// writeSSHKey stores a throwaway ed25519 private key and returns its path.
func writeSSHKey() string {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	Expect(err).NotTo(HaveOccurred())
	block, err := ssh.MarshalPrivateKey(key, "")
	Expect(err).NotTo(HaveOccurred())
	path := filepath.Join(GinkgoT().TempDir(), "id_ed25519")
	Expect(os.WriteFile(path, pem.EncodeToMemory(block), 0o600)).To(Succeed())
	return path
}

func readFile(path string) string {
	data, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())
	return string(data)
}

var _ = Describe("Git client", func() {
	var (
		ctx    context.Context
		remote *upstream
		cfg    Config
	)

	BeforeEach(func() {
		ctx = context.Background()
		remote = newUpstream()
		remote.commit(map[string]*string{
			"stacks/app/compose.yml": content("services: {}\n"),
			"stacks/db/compose.yml":  content("services: {}\n"),
			"README.md":              content("readme\n"),
		})
		cfg = Config{
			RepoURL:    remote.dir,
			SSHKeyPath: writeSSHKey(),
			ClonePath:  filepath.Join(GinkgoT().TempDir(), "clone"),
			Branch:     "main",
		}
	})

	Describe("New", func() {
		It("clones the repository when the clone path is empty", func() {
			client, err := New(cfg)
			Expect(err).NotTo(HaveOccurred())
			Expect(client.Path()).To(Equal(cfg.ClonePath))
			Expect(readFile(filepath.Join(cfg.ClonePath, "README.md"))).To(Equal("readme\n"))
		})

		It("fails when the SSH key cannot be loaded", func() {
			cfg.SSHKeyPath = filepath.Join(GinkgoT().TempDir(), "missing")
			_, err := New(cfg)
			Expect(err).To(MatchError(ContainSubstring("failed to load SSH key")))
		})

		It("fails when the remote cannot be cloned", func() {
			cfg.RepoURL = filepath.Join(GinkgoT().TempDir(), "missing")
			_, err := New(cfg)
			Expect(err).To(MatchError(ContainSubstring("failed to clone")))
		})

		Context("with an existing clone", func() {
			BeforeEach(func() {
				_, err := New(cfg)
				Expect(err).NotTo(HaveOccurred())
			})

			It("discards tracked changes and preserves untracked files", func() {
				Expect(os.WriteFile(filepath.Join(cfg.ClonePath, "README.md"), []byte("local edit\n"), 0o644)).To(Succeed())
				Expect(os.Remove(filepath.Join(cfg.ClonePath, "stacks/db/compose.yml"))).To(Succeed())
				untracked := filepath.Join(cfg.ClonePath, "stacks/app/.env")
				Expect(os.WriteFile(untracked, []byte("LOCAL=1\n"), 0o600)).To(Succeed())

				_, err := New(cfg)
				Expect(err).NotTo(HaveOccurred())
				Expect(readFile(filepath.Join(cfg.ClonePath, "README.md"))).To(Equal("readme\n"))
				Expect(filepath.Join(cfg.ClonePath, "stacks/db/compose.yml")).To(BeARegularFile())
				Expect(readFile(untracked)).To(Equal("LOCAL=1\n"))
			})

			It("creates a local branch from a branch that exists only on the remote", func() {
				w, err := remote.repo.Worktree()
				Expect(err).NotTo(HaveOccurred())
				Expect(w.Checkout(&git.CheckoutOptions{Branch: plumbing.NewBranchReferenceName("dev"), Create: true})).To(Succeed())
				remote.commit(map[string]*string{"dev.txt": content("dev\n")})

				cfg.Branch = "dev"
				_, err = New(cfg)
				Expect(err).NotTo(HaveOccurred())
				Expect(readFile(filepath.Join(cfg.ClonePath, "dev.txt"))).To(Equal("dev\n"))
			})

			It("fails when the branch does not exist on the remote", func() {
				cfg.Branch = "missing"
				_, err := New(cfg)
				Expect(err).To(MatchError(ContainSubstring(`branch "missing" not found on remote`)))
			})

			It("fails when the remote is unreachable while looking up a new branch", func() {
				Expect(os.RemoveAll(remote.dir)).To(Succeed())
				cfg.Branch = "other"
				_, err := New(cfg)
				Expect(err).To(MatchError(ContainSubstring("failed to fetch")))
			})
		})
	})

	Describe("synchronization", func() {
		var gitClient Client

		BeforeEach(func() {
			var err error
			gitClient, err = New(cfg)
			Expect(err).NotTo(HaveOccurred())
		})

		Describe("HasUpdates", func() {
			It("reports no updates when the remote is unchanged", func() {
				hasUpdates, remoteSHA, localSHA, err := gitClient.HasUpdates(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(hasUpdates).To(BeFalse())
				Expect(remoteSHA).To(HaveLen(7))
				Expect(remoteSHA).To(Equal(localSHA))
			})

			It("reports updates with short SHAs when the remote has new commits", func() {
				newSHA := remote.commit(map[string]*string{"README.md": content("changed\n")})

				hasUpdates, remoteSHA, localSHA, err := gitClient.HasUpdates(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(hasUpdates).To(BeTrue())
				Expect(remoteSHA).To(Equal(newSHA[:7]))
				Expect(localSHA).To(HaveLen(7))
				Expect(localSHA).NotTo(Equal(remoteSHA))
			})

			It("returns fetch errors", func() {
				Expect(os.RemoveAll(remote.dir)).To(Succeed())

				hasUpdates, remoteSHA, localSHA, err := gitClient.HasUpdates(ctx)
				Expect(err).To(HaveOccurred())
				Expect(hasUpdates).To(BeFalse())
				Expect(remoteSHA).To(BeEmpty())
				Expect(localSHA).NotTo(BeEmpty())
			})
		})

		Describe("Pull", func() {
			It("returns no changes when already up to date", func() {
				changes, err := gitClient.Pull(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(changes).To(BeEmpty())
			})

			It("returns sorted created, updated, deleted, and renamed files and updates the worktree", func() {
				remote.commit(map[string]*string{
					"stacks/app/compose.yml": content("services:\n  web:\n    image: nginx\n"),
					"stacks/new/compose.yml": content("services: {}\n"),
					"stacks/db/compose.yml":  nil,
				})
				w, err := remote.repo.Worktree()
				Expect(err).NotTo(HaveOccurred())
				Expect(os.Rename(filepath.Join(remote.dir, "README.md"), filepath.Join(remote.dir, "docs.md"))).To(Succeed())
				_, err = w.Remove("README.md")
				Expect(err).NotTo(HaveOccurred())
				remote.commit(map[string]*string{"docs.md": content("readme\n")})

				changes, err := gitClient.Pull(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(changes).To(Equal([]FileChange{
					{Path: "README.md", Action: ChangeActionDelete},
					{Path: "docs.md", Action: ChangeActionCreate},
					{Path: "stacks/app/compose.yml", Action: ChangeActionUpdate},
					{Path: "stacks/db/compose.yml", Action: ChangeActionDelete},
					{Path: "stacks/new/compose.yml", Action: ChangeActionCreate},
				}))
				Expect(readFile(filepath.Join(cfg.ClonePath, "stacks/app/compose.yml"))).To(ContainSubstring("nginx"))
				Expect(filepath.Join(cfg.ClonePath, "stacks/db/compose.yml")).NotTo(BeAnExistingFile())
			})

			It("diffs against local HEAD after HasUpdates advanced the remote ref", func() {
				remote.commit(map[string]*string{"README.md": content("changed\n")})
				hasUpdates, _, _, err := gitClient.HasUpdates(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(hasUpdates).To(BeTrue())

				changes, err := gitClient.Pull(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(changes).To(Equal([]FileChange{{Path: "README.md", Action: ChangeActionUpdate}}))
			})

			It("discards local tracked edits and preserves untracked files", func() {
				remote.commit(map[string]*string{"stacks/app/compose.yml": content("services: {web: {image: nginx}}\n")})
				Expect(os.WriteFile(filepath.Join(cfg.ClonePath, "README.md"), []byte("local\n"), 0o644)).To(Succeed())
				untracked := filepath.Join(cfg.ClonePath, "notes.txt")
				Expect(os.WriteFile(untracked, []byte("keep\n"), 0o644)).To(Succeed())

				_, err := gitClient.Pull(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(readFile(filepath.Join(cfg.ClonePath, "README.md"))).To(Equal("readme\n"))
				Expect(readFile(untracked)).To(Equal("keep\n"))
			})

			It("returns fetch errors", func() {
				Expect(os.RemoveAll(remote.dir)).To(Succeed())

				_, err := gitClient.Pull(ctx)
				Expect(err).To(MatchError(ContainSubstring("failed to fetch")))
			})
		})
	})
})
