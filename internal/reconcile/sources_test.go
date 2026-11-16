package reconcile

import (
	"context"
	"os"
	"path/filepath"

	"github.com/compose-spec/compose-go/v2/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("collectComposeSources", func() {
	var (
		ctx context.Context
		f   *fixture
	)

	collect := func(stack string, environment types.Mapping, loadEnvironment directoryEnvironment) (composeSources, error) {
		dir := filepath.Join(f.stackRoot, stack)
		return collectComposeSources(ctx, f.repo, []string{"compose.yml"}, dir, environment, loadEnvironment)
	}

	BeforeEach(func() {
		ctx = context.Background()
		f = newFixture()
	})

	It("follows local includes, extends chains, and include environment files", func() {
		compose := f.stack("app", `
include:
  - path: ${SHARED}/db.yml
  - path: ../shared/cache.yml
    env_file: [../shared/cache.env, /dev/null]
    project_directory: ../shared
  - oci://registry.example.com/remote:latest
services:
  web:
    extends:
      file: base.yml
      service: base
  oci:
    extends:
      file: oci://registry.example.com/base:latest
      service: base
  git:
    extends:
      file: git@github.com:example/base.git
      service: base
  plain:
    image: nginx
`)
		db := f.write("stacks/shared/db.yml", "services:\n  db:\n    image: postgres\n")
		dbEnv := f.write("stacks/shared/.env", "TAG=1\n")
		cache := f.write("stacks/shared/cache.yml", "services:\n  cache:\n    image: redis\n")
		cacheEnv := f.write("stacks/shared/cache.env", "TAG=2\n")
		base := f.write("stacks/app/base.yml", "services:\n  base:\n    extends:\n      file: ../common/root.yml\n      service: root\n")
		root := f.write("stacks/common/root.yml", "services:\n  root:\n    image: busybox\n")

		sources, err := collect("app", types.Mapping{"SHARED": "../shared"}, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(sources.composeFiles).To(Equal([]string{compose, db, cache}))
		Expect(sources.dependencyFiles).To(ConsistOf(compose, db, cache, base, root))
		Expect(sources.envFiles).To(ConsistOf(dbEnv, cacheEnv))
		Expect(sources.optionalFiles).To(Equal(map[string]struct{}{dbEnv: {}}))
	})

	It("tracks the missing default include .env as optional", func() {
		f.stack("app", "include:\n  - ../shared/db.yml\n")
		f.write("stacks/shared/db.yml", "services: {}\n")
		missingEnv := filepath.Join(f.stackRoot, "shared", ".env")

		sources, err := collect("app", types.Mapping{}, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(sources.envFiles).To(Equal([]string{missingEnv}))
		Expect(sources.optionalFiles).To(HaveKey(missingEnv))
	})

	It("records both lexical and canonical paths for symlinked sources", func() {
		target := f.write("stacks/shared/real.yml", "services: {}\n")
		link := filepath.Join(f.stackRoot, "app", "linked.yml")
		f.stack("app", "include:\n  - linked.yml\n")
		Expect(os.Symlink(target, link)).To(Succeed())

		sources, err := collect("app", types.Mapping{}, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(sources.dependencyFiles).To(ContainElements(link, target))
	})

	It("terminates on include cycles", func() {
		f.stack("app", "include:\n  - ../shared/a.yml\n")
		f.write("stacks/shared/a.yml", "include:\n  - b.yml\n")
		f.write("stacks/shared/b.yml", "include:\n  - a.yml\n")

		sources, err := collect("app", types.Mapping{}, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(sources.dependencyFiles).To(HaveLen(3))
	})

	It("applies directory environments to the stack, includes, and extends", func() {
		f.stack("app", "include:\n  - ../shared/db.yml\nservices:\n  web:\n    extends:\n      file: ../common/base.yml\n      service: base\n")
		f.write("stacks/shared/db.yml", "services: {}\n")
		f.write("stacks/common/base.yml", "services:\n  base:\n    image: nginx\n")
		var dirs []string

		_, err := collect("app", types.Mapping{}, func(dir string, environment types.Mapping) (types.Mapping, error) {
			dirs = append(dirs, dir)
			return environment, nil
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(dirs).To(Equal([]string{
			filepath.Join(f.stackRoot, "app"),
			filepath.Join(f.stackRoot, "shared"),
			filepath.Join(f.stackRoot, "common"),
		}))
	})

	DescribeTable("rejects invalid sources",
		func(compose string, extra map[string]string, expected string) {
			f.stack("app", compose)
			for path, content := range extra {
				f.write(path, content)
			}
			_, err := collect("app", types.Mapping{}, nil)
			Expect(err).To(MatchError(ContainSubstring(expected)))
		},
		Entry("malformed YAML", "services: [\n", nil, "failed to load compose sources"),
		Entry("include outside the repository", "include:\n  - ../../../outside.yml\n", nil, "invalid included compose file path"),
		Entry("missing include", "include:\n  - missing.yml\n", nil, "invalid included compose file path"),
		Entry("include project directory outside the repository",
			"include:\n  - path: ../shared/db.yml\n    project_directory: ../../..\n",
			map[string]string{"stacks/shared/db.yml": "services: {}\n"}, "invalid include project directory"),
		Entry("include env_file outside the repository",
			"include:\n  - path: ../shared/db.yml\n    env_file: ../../../outside.env\n",
			map[string]string{"stacks/shared/db.yml": "services: {}\n"}, "invalid include environment file path"),
		Entry("extends outside the repository",
			"services:\n  web:\n    extends:\n      file: ../../../outside.yml\n      service: base\n", nil, "invalid extends compose file path"),
	)

	It("rejects compose files outside the repository", func() {
		_, err := collectComposeSources(ctx, f.repo, []string{"/etc/hosts"}, f.stackRoot, types.Mapping{}, nil)
		Expect(err).To(MatchError(ContainSubstring("invalid compose file path")))
	})

	It("rejects missing roots and working directories", func() {
		_, err := collectComposeSources(ctx, filepath.Join(f.repo, "missing"), nil, f.repo, nil, nil)
		Expect(err).To(HaveOccurred())

		_, err = collectComposeSources(ctx, f.repo, nil, filepath.Join(f.repo, "missing"), nil, nil)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("interpolateSourceReferences", func() {
	It("interpolates only include and extends declarations", func() {
		model := map[string]any{
			"include": []any{"${DIR}/a.yml"},
			"services": map[string]any{
				"web":  map[string]any{"image": "${MISSING?required}", "extends": map[string]any{"file": "${DIR}/base.yml"}},
				"bad":  "not-a-map",
				"none": map[string]any{"image": "nginx"},
			},
		}

		interpolated, err := interpolateSourceReferences(model, types.Mapping{"DIR": "shared"})
		Expect(err).NotTo(HaveOccurred())
		Expect(interpolated).To(Equal(map[string]any{
			"include":  []any{"shared/a.yml"},
			"services": map[string]any{"web": map[string]any{"extends": map[string]any{"file": "shared/base.yml"}}},
		}))
	})
})

var _ = Describe("sourceWalkKey", func() {
	key := func(files []string, dir string, environment types.Mapping) string {
		return sourceWalkKey(files, dir, environment)
	}

	It("distinguishes files, directories, and environment values", func() {
		base := key([]string{"a", "b"}, "/dir", types.Mapping{"A": "1"})
		Expect(key([]string{"ab"}, "/dir", types.Mapping{"A": "1"})).NotTo(Equal(base))
		Expect(key([]string{"a", "b"}, "/other", types.Mapping{"A": "1"})).NotTo(Equal(base))
		Expect(key([]string{"a", "b"}, "/dir", types.Mapping{"A": "2"})).NotTo(Equal(base))
	})
})
