package reconcile

import (
	"os"
	"path/filepath"

	"github.com/compose-spec/compose-go/v2/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/veerendra2/composeflux/pkg/gitrepo"
)

var _ = DescribeTable("pathWithinRoot",
	func(path string, expected bool) {
		Expect(pathWithinRoot("/repo", path)).To(Equal(expected))
	},
	Entry("root itself", "/repo", true),
	Entry("nested path", "/repo/stacks/app", true),
	Entry("dot-dot prefixed name inside root", "/repo/..data", true),
	Entry("parent", "/", false),
	Entry("sibling with shared prefix", "/repository", false),
	Entry("traversal", "/repo/../etc", false),
	Entry("relative path", "repo", false),
)

var _ = DescribeTable("pathContains",
	func(path string, expected bool) {
		Expect(pathContains("/repo/data", path)).To(Equal(expected))
	},
	Entry("same path", "/repo/data", true),
	Entry("nested path", "/repo/data/file", true),
	Entry("shared prefix", "/repo/database", false),
	Entry("parent", "/repo", false),
)

var _ = Describe("resolvePathsWithinRoot", func() {
	var root string

	BeforeEach(func() {
		var err error
		root, err = filepath.EvalSymlinks(GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		Expect(os.MkdirAll(filepath.Join(root, "real"), 0o755)).To(Succeed())
	})

	It("returns lexical and canonical paths for symlinks inside the root", func() {
		Expect(os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link"))).To(Succeed())

		lexical, resolved, err := resolvePathsWithinRoot(root, filepath.Join(root, "link"))
		Expect(err).NotTo(HaveOccurred())
		Expect(lexical).To(Equal(filepath.Join(root, "link")))
		Expect(resolved).To(Equal(filepath.Join(root, "real")))
	})

	It("rejects lexical traversal", func() {
		_, err := resolvePathWithinRoot(root, filepath.Join(root, "..", "other"))
		Expect(err).To(MatchError(ContainSubstring("is outside repository")))
	})

	It("rejects symlinks resolving outside the root", func() {
		Expect(os.Symlink(GinkgoT().TempDir(), filepath.Join(root, "escape"))).To(Succeed())

		_, err := resolvePathWithinRoot(root, filepath.Join(root, "escape"))
		Expect(err).To(MatchError(ContainSubstring("resolves outside repository")))
	})

	It("fails for missing paths and roots", func() {
		_, err := resolvePathWithinRoot(root, filepath.Join(root, "missing"))
		Expect(err).To(MatchError(os.ErrNotExist))

		_, err = resolvePathWithinRoot(filepath.Join(root, "missing"), filepath.Join(root, "missing"))
		Expect(err).To(MatchError(os.ErrNotExist))
	})
})

var _ = Describe("projectDependencyPaths", func() {
	It("collects file, bind, build-context, and Dockerfile references", func() {
		project := &types.Project{
			WorkingDir:   "/repo/app",
			ComposeFiles: []string{"/repo/app/compose.yml"},
			Configs:      types.Configs{"cfg": {File: "config.txt"}, "external": {}},
			Secrets:      types.Secrets{"secret": {File: "/repo/secret.txt"}},
			Services: types.Services{
				"web": {
					EnvFiles: []types.EnvFile{{Path: "web.env", Required: true}, {Path: "optional.env"}},
					Volumes: []types.ServiceVolumeConfig{
						{Type: types.VolumeTypeBind, Source: "/repo/app/data"},
						{Type: types.VolumeTypeVolume, Source: "named"},
					},
					Extends: &types.ExtendsConfig{File: "base.yml"},
					Build:   &types.BuildConfig{Context: "/repo/app/web"},
				},
				"api":    {Build: &types.BuildConfig{Context: "/repo/app/api", Dockerfile: "/repo/app/api.Dockerfile"}},
				"inline": {Build: &types.BuildConfig{Context: "/repo/app/inline", DockerfileInline: "FROM scratch"}},
				"custom": {Build: &types.BuildConfig{Context: "/repo/app/custom", Dockerfile: "build/Containerfile"}},
			},
		}

		paths := projectDependencyPaths(project)
		Expect(paths.filePaths).To(ConsistOf(
			"/repo/app/.env",
			"/repo/app/compose.yml",
			"/repo/app/config.txt",
			"/repo/secret.txt",
			"/repo/app/web.env",
			"/repo/app/optional.env",
			"/repo/app/base.yml",
			"/repo/app/web/Dockerfile",
			"/repo/app/api.Dockerfile",
			"/repo/app/custom/build/Containerfile",
		))
		Expect(paths.bindPaths).To(ConsistOf("/repo/app/data"))
		Expect(paths.buildContexts).To(ConsistOf("/repo/app/web", "/repo/app/api", "/repo/app/inline", "/repo/app/custom"))
		Expect(paths.dockerfiles).To(HaveLen(3))
		Expect(paths.dockerfiles).To(HaveKey("/repo/app/api.Dockerfile"))
		Expect(paths.optionalFiles).To(HaveLen(2))
		Expect(paths.optionalFiles).To(HaveKey("/repo/app/.env"))
		Expect(paths.optionalFiles).To(HaveKey("/repo/app/optional.env"))
		Expect(projectBuildsImages(project)).To(BeTrue())
	})
})

var _ = Describe("buildStackDependencies", func() {
	var repo string

	write := func(path string) string {
		path = filepath.Join(repo, path)
		Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
		Expect(os.WriteFile(path, nil, 0o644)).To(Succeed())
		return path
	}

	BeforeEach(func() {
		var err error
		repo, err = filepath.EvalSymlinks(GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
	})

	It("classifies existing, missing, and external dependencies", func() {
		compose := write("app/compose.yml")
		write("app/data/file")
		bindFile := write("app/config.conf")
		dockerfile := write("app/web/Dockerfile")
		dataDir := filepath.Join(repo, "app/data")
		missingBind := filepath.Join(repo, "app/missing")
		project := &types.Project{
			Name:         "app",
			WorkingDir:   filepath.Join(repo, "app"),
			ComposeFiles: []string{compose},
			Services: types.Services{"web": {
				Volumes: []types.ServiceVolumeConfig{
					{Type: types.VolumeTypeBind, Source: dataDir},
					{Type: types.VolumeTypeBind, Source: bindFile},
					{Type: types.VolumeTypeBind, Source: missingBind},
					{Type: types.VolumeTypeBind, Source: "/var/run/docker.sock"},
				},
				Build: &types.BuildConfig{Context: filepath.Join(repo, "app/web")},
			}, "ghost": {Build: &types.BuildConfig{Context: filepath.Join(repo, "app/missing-context")}},
				"external": {Build: &types.BuildConfig{Context: "/outside/context"}}},
		}
		external := "/etc/hosts"

		deps := buildStackDependencies(repo, project, []string{compose, external, dataDir}, []string{compose, external}, []string{"/repo/secrets/"}, nil)

		Expect(deps.filePaths).To(ContainElements(compose, bindFile, dockerfile, filepath.Join(repo, "app/.env")))
		Expect(deps.filePaths).NotTo(ContainElement(external))
		Expect(deps.directoryPaths).To(ConsistOf(dataDir, dataDir, missingBind))
		Expect(deps.buildContexts).To(ConsistOf(filepath.Join(repo, "app/web"), filepath.Join(repo, "app/missing-context")))
		Expect(deps.rebuildFiles).To(Equal(map[string]struct{}{compose: {}}))
		Expect(deps.dockerfiles).To(HaveKey(dockerfile))
		Expect(deps.localSecretDirs).To(HaveKey("/repo/secrets"))
	})

	It("does not mark Compose files as rebuild sources without build services", func() {
		compose := write("app/compose.yml")
		project := &types.Project{Name: "app", WorkingDir: filepath.Join(repo, "app"), Services: types.Services{"web": {Image: "nginx"}}}

		deps := buildStackDependencies(repo, project, nil, []string{compose}, nil, map[string]struct{}{})
		Expect(deps.rebuildFiles).To(BeEmpty())
	})
})

var _ = Describe("stackDependencies.impact", func() {
	var deps stackDependencies

	BeforeEach(func() {
		deps = stackDependencies{
			filePaths:       []string{"/repo/app/compose.yml", "/repo/app/Dockerfile", "/repo/app/web.env", "/repo/app/data/seed.sql"},
			directoryPaths:  []string{"/repo/app/data"},
			buildContexts:   []string{"/repo/app"},
			rebuildFiles:    map[string]struct{}{"/repo/app/compose.yml": {}},
			dockerfiles:     map[string]struct{}{"/repo/app/Dockerfile": {}},
			localSecretDirs: map[string]struct{}{"/repo/secrets": {}},
			isLocalSecret:   func(path string) bool { return filepath.Ext(path) == ".age" },
		}
	})

	DescribeTable("classifies a single changed path",
		func(path string, deploy, build bool, reason string) {
			impact := deps.impact(map[string]gitrepo.ChangeAction{path: gitrepo.ChangeActionUpdate})
			Expect(impact.deploy).To(Equal(deploy))
			Expect(impact.build).To(Equal(build))
			if reason == "" {
				Expect(impact.matches).To(BeEmpty())
			} else {
				Expect(impact.matches).To(ConsistOf(changeMatch{path: path, action: gitrepo.ChangeActionUpdate, reason: reason, rebuild: build}))
			}
		},
		Entry("Dockerfile", "/repo/app/Dockerfile", true, true, "dockerfile"),
		Entry("Compose build source", "/repo/app/compose.yml", true, true, "compose_build_source"),
		Entry("file inside a build context", "/repo/app/web.env", true, true, "dependency_file"),
		Entry("bind-mounted directory inside a build context", "/repo/app/data/new.txt", true, true, "build_context"),
		Entry("tracked file in a mounted directory", "/repo/app/data/seed.sql", true, true, "dependency_file"),
		Entry("local secret in a tracked directory", "/repo/secrets/app.age", true, false, "local_secret"),
		Entry("local secret in another directory", "/repo/other/app.age", false, false, ""),
		Entry("unrelated file", "/repo/README.md", false, false, ""),
	)

	It("reports directory dependencies outside build contexts without rebuilding", func() {
		deps.buildContexts = nil
		impact := deps.impact(map[string]gitrepo.ChangeAction{"/repo/app/data/new.txt": gitrepo.ChangeActionCreate})
		Expect(impact.deploy).To(BeTrue())
		Expect(impact.build).To(BeFalse())
		Expect(impact.matches[0].reason).To(Equal("dependency_directory"))
	})

	It("combines matches in sorted path order", func() {
		impact := deps.impact(map[string]gitrepo.ChangeAction{
			"/repo/secrets/app.age": gitrepo.ChangeActionDelete,
			"/repo/README.md":       gitrepo.ChangeActionUpdate,
			"/repo/app/Dockerfile":  gitrepo.ChangeActionUpdate,
		})
		Expect(impact.deploy).To(BeTrue())
		Expect(impact.build).To(BeTrue())
		Expect(impact.matches).To(HaveLen(2))
		Expect(impact.matches[0].path).To(Equal("/repo/app/Dockerfile"))
		Expect(impact.matches[1].path).To(Equal("/repo/secrets/app.age"))
	})
})
