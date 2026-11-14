package reconcile

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	"github.com/veerendra2/composeflux/pkg/gitrepo"
)

var _ = Describe("Run", func() {
	const tick = 10 * time.Millisecond

	var f *fixture

	// start runs the reconciler until the spec ends and returns its result channel.
	start := func(r *Reconciler) <-chan error {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			defer GinkgoRecover()
			done <- r.Run(ctx)
		}()
		DeferCleanup(func() {
			cancel()
			Eventually(done).Should(Receive(BeNil()))
		})
		return done
	}

	signal := func(ch chan struct{}) {
		select {
		case ch <- struct{}{}:
		default:
		}
	}

	BeforeEach(func() {
		f = newFixture()
		f.stack("app", webService)
	})

	It("fails when the initial sync fails", func() {
		f.git.EXPECT().Pull(gomock.Any()).Return(nil, errors.New("network down"))
		Expect(f.reconciler().Run(context.Background())).To(MatchError("failed initial sync: network down"))
	})

	It("syncs when Git has updates and retries pending syncs without fetching", func() {
		f.running(map[string][]api.ContainerSummary{"app": healthy()})
		r := f.reconciler(func(r *Reconciler) { r.gitInterval = tick })
		deployed := make(chan struct{}, 1)
		gomock.InOrder(
			f.git.EXPECT().Pull(gomock.Any()).Return(nil, nil),
			f.git.EXPECT().HasUpdates(gomock.Any()).Return(false, "", "", errors.New("fetch failed")),
			f.git.EXPECT().HasUpdates(gomock.Any()).Return(true, "remote", "local", nil),
			f.git.EXPECT().Pull(gomock.Any()).Return([]gitrepo.FileChange{{Path: "stacks/app/compose.yml", Action: gitrepo.ChangeActionUpdate}}, nil),
			f.docker.EXPECT().Up(gomock.Any(), named("app")).Return(errors.New("port in use")),
			f.git.EXPECT().Pull(gomock.Any()).Return(nil, nil),
			f.docker.EXPECT().Up(gomock.Any(), named("app")).DoAndReturn(func(context.Context, *types.Project) error {
				signal(deployed)
				return nil
			}),
			f.git.EXPECT().HasUpdates(gomock.Any()).Return(false, "local", "local", nil).AnyTimes(),
		)

		start(r)
		Eventually(deployed).Should(Receive())
	})

	It("runs health reconciliation and resource pruning on their intervals", func() {
		f.running(map[string][]api.ContainerSummary{"app": healthy()})
		r := f.reconciler(func(r *Reconciler) {
			r.gitInterval = time.Hour
			r.healthInterval = tick
			r.pruneInterval = tick
		})
		pruned := make(chan struct{}, 1)
		f.git.EXPECT().Pull(gomock.Any()).Return(nil, nil)
		f.docker.EXPECT().Prune(gomock.Any()).Do(func(context.Context) { signal(pruned) }).MinTimes(1)

		start(r)
		Eventually(pruned).Should(Receive())
	})

	It("keeps running when periodic health and prune passes fail", func() {
		r := f.reconciler(func(r *Reconciler) {
			r.gitInterval = time.Hour
			r.healthInterval = tick
			r.pruneInterval = tick
		})
		var failures atomic.Int32
		f.git.EXPECT().Pull(gomock.Any()).Return(nil, nil)
		f.docker.EXPECT().Ps(gomock.Any(), "app").Return(healthy(), nil)
		gomock.InOrder(
			// The initial sync lists stacks for state and pruning.
			f.docker.EXPECT().List(gomock.Any()).Return([]api.Stack{{Name: "app"}}, nil).Times(2),
			f.docker.EXPECT().List(gomock.Any()).DoAndReturn(func(context.Context) ([]api.Stack, error) {
				failures.Add(1)
				return nil, errors.New("daemon down")
			}).MinTimes(1),
		)

		done := start(r)
		Eventually(failures.Load).Should(BeNumerically(">=", 4))
		Expect(done).NotTo(Receive())
	})

	It("runs image updates on the cron schedule and survives failed checks", func() {
		f.running(map[string][]api.ContainerSummary{"app": healthy()})
		r := f.reconciler(f.withRemoteSecrets, func(r *Reconciler) {
			r.gitInterval = time.Hour
			r.imageUpdateSchedule = "@every 1s"
		})
		checked := make(chan struct{}, 1)
		f.git.EXPECT().Pull(gomock.Any()).Return(nil, nil)
		gomock.InOrder(
			f.remote.EXPECT().FetchAll().Return(nil, nil),
			f.remote.EXPECT().FetchAll().Return(nil, errors.New("unauthorized")),
			f.remote.EXPECT().FetchAll().Return(nil, nil).AnyTimes(),
		)
		f.docker.EXPECT().HasImageUpdates(gomock.Any(), named("app")).DoAndReturn(
			func(context.Context, *types.Project) (bool, error) {
				signal(checked)
				return false, nil
			}).MinTimes(1)

		start(r)
		Eventually(checked, 5*time.Second).Should(Receive())
	})
})
