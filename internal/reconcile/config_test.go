package reconcile

import (
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = DescribeTable("Config.Validate",
	func(mutate func(*Config), expectedError string) {
		cfg := Config{StackPath: "stacks", GitInterval: time.Minute}
		mutate(&cfg)
		err := cfg.Validate()
		if expectedError == "" {
			Expect(err).NotTo(HaveOccurred())
		} else {
			Expect(err).To(MatchError(ContainSubstring(expectedError)))
		}
	},
	Entry("minimal valid configuration", func(*Config) {}, ""),
	Entry("all schedules enabled", func(c *Config) {
		c.HealthInterval = time.Minute
		c.PruneInterval = time.Hour
		c.ImageUpdateSchedule = "0 3 * * 1"
	}, ""),
	Entry("zero Git interval", func(c *Config) { c.GitInterval = 0 }, "--git-interval must be greater than zero"),
	Entry("negative Git interval", func(c *Config) { c.GitInterval = -time.Second }, "--git-interval must be greater than zero"),
	Entry("negative health interval", func(c *Config) { c.HealthInterval = -time.Second }, "--health-interval must not be negative"),
	Entry("negative prune interval", func(c *Config) { c.PruneInterval = -time.Second }, "--prune-interval must not be negative"),
	Entry("invalid cron expression", func(c *Config) { c.ImageUpdateSchedule = "not a cron" }, "invalid --image-update-schedule"),
)

var _ = Describe("New", func() {
	It("rejects invalid configuration", func() {
		_, err := New(Config{}, nil, nil, nil, nil)
		Expect(err).To(MatchError(ContainSubstring("--git-interval")))
	})
})

var _ = Describe("stack configuration", func() {
	var (
		f *fixture
		r *Reconciler
	)

	BeforeEach(func() {
		f = newFixture()
		r = f.reconciler()
	})

	It("treats a missing stack.yml as empty configuration", func() {
		envs, order, path, err := r.loadStackConfig(f.stackRoot)
		Expect(err).NotTo(HaveOccurred())
		Expect(envs).To(BeEmpty())
		Expect(order).To(BeEmpty())
		Expect(path).To(Equal(filepath.Join(f.stackRoot, "stack.yml")))
	})

	It("loads environment variables and startup order", func() {
		f.write("stacks/stack.yml", "startup_order: [db, web]\nenvs:\n  DOMAIN: example.com\n  TZ: UTC\n")

		envs, order, _, err := r.loadStackConfig(f.stackRoot)
		Expect(err).NotTo(HaveOccurred())
		Expect(envs).To(ConsistOf("DOMAIN=example.com", "TZ=UTC"))
		Expect(order).To(Equal([]string{"db", "web"}))
	})

	It("fails on malformed YAML instead of using empty configuration", func() {
		f.write("stacks/stack.yml", "envs: [unterminated\n")

		_, _, _, err := r.loadStackConfig(f.stackRoot)
		Expect(err).To(MatchError(ContainSubstring("failed to load stack config")))
	})

	It("rejects a configuration file outside the stack root", func() {
		r.configFile = "../stack.yml"

		_, _, _, err := r.loadStackConfig(f.stackRoot)
		Expect(err).To(MatchError(ContainSubstring("outside stack root")))
	})

	It("resolves a configuration symlink within the stack root", func() {
		target := f.write("stacks/config/real.yml", "envs:\n  A: \"1\"\n")
		Expect(os.Symlink(target, filepath.Join(f.stackRoot, "stack.yml"))).To(Succeed())

		envs, _, path, err := r.loadStackConfig(f.stackRoot)
		Expect(err).NotTo(HaveOccurred())
		Expect(envs).To(Equal([]string{"A=1"}))
		Expect(path).To(Equal(target))
	})

	It("rejects a configuration symlink escaping the stack root", func() {
		target := f.write("outside.yml", "envs: {}\n")
		Expect(os.Symlink(target, filepath.Join(f.stackRoot, "stack.yml"))).To(Succeed())

		_, err := r.stackConfigPath(f.stackRoot)
		Expect(err).To(MatchError(ContainSubstring("resolves outside")))
	})

	It("rejects a configuration directory symlink escaping the stack root", func() {
		Expect(os.Symlink(f.repo, filepath.Join(f.stackRoot, "linked"))).To(Succeed())
		r.configFile = "linked/stack.yml"

		_, err := r.stackConfigPath(f.stackRoot)
		Expect(err).To(MatchError(ContainSubstring("invalid config directory")))
	})

	Describe("sourceRoots", func() {
		It("returns canonical repository and stack roots", func() {
			repo, stackRoot, err := r.sourceRoots()
			Expect(err).NotTo(HaveOccurred())
			Expect(repo).To(Equal(f.repo))
			Expect(stackRoot).To(Equal(f.stackRoot))
		})

		It("rejects a stack path outside the repository", func() {
			r.stackPath = "../elsewhere"
			_, _, err := r.sourceRoots()
			Expect(err).To(MatchError(ContainSubstring("invalid stack path")))
		})

		It("rejects a missing stack path", func() {
			r.stackPath = "missing"
			_, _, err := r.sourceRoots()
			Expect(err).To(MatchError(ContainSubstring("invalid stack path")))
		})
	})
})
