package remotesecrets

import (
	"errors"

	"github.com/bitwarden/sdk-go/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("bitwardenClient", func() {
	var (
		bw      *MockBitwardenClient
		secrets *MockBitwardenSecrets
		client  *bitwardenClient
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		bw = NewMockBitwardenClient(ctrl)
		secrets = NewMockBitwardenSecrets(ctrl)
		bw.EXPECT().Secrets().Return(secrets).AnyTimes()
		client = &bitwardenClient{organizationID: "org", projectID: "project", bwClient: bw}
	})

	Describe("FetchAll", func() {
		It("returns only secrets belonging to the configured project", func() {
			project, other := "project", "other"
			secrets.EXPECT().Sync("org", nil).Return(&sdk.SecretsSyncResponse{Secrets: []sdk.SecretResponse{
				{Key: "DB_PASSWORD", Value: "hunter2", ProjectID: &project},
				{Key: "EMPTY", Value: "", ProjectID: &project},
				{Key: "FOREIGN", Value: "x", ProjectID: &other},
				{Key: "UNASSIGNED", Value: "y"},
			}}, nil)

			result, err := client.FetchAll()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(HaveLen(2))
			Expect(*result["DB_PASSWORD"]).To(Equal("hunter2"))
			Expect(*result["EMPTY"]).To(BeEmpty())
		})

		It("returns sync errors", func() {
			secrets.EXPECT().Sync("org", nil).Return(nil, errors.New("unauthorized"))

			_, err := client.FetchAll()
			Expect(err).To(MatchError("unauthorized"))
		})
	})

	Describe("Get", func() {
		It("returns the secret value by ID", func() {
			secrets.EXPECT().Get("id-1").Return(&sdk.SecretResponse{Value: "key-material"}, nil)

			Expect(client.Get("id-1")).To(Equal("key-material"))
		})

		It("returns lookup errors", func() {
			secrets.EXPECT().Get("missing").Return(nil, errors.New("not found"))

			_, err := client.Get("missing")
			Expect(err).To(MatchError("not found"))
		})
	})
})
