package localsecrets

import (
	"bytes"
	"io"
	"os"
	"path/filepath"

	"filippo.io/age"
	"filippo.io/age/armor"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const testPassphrase = "correct horse battery staple"

// encryptEnv returns plaintext encrypted for passphrase, optionally ASCII-armored.
func encryptEnv(passphrase, plaintext string, armored bool) []byte {
	recipient, err := age.NewScryptRecipient(passphrase)
	Expect(err).NotTo(HaveOccurred())
	recipient.SetWorkFactor(10) // keep scrypt fast in tests

	var out bytes.Buffer
	var dst io.WriteCloser = nopCloser{&out}
	if armored {
		dst = armor.NewWriter(&out)
	}
	w, err := age.Encrypt(dst, recipient)
	Expect(err).NotTo(HaveOccurred())
	_, err = io.WriteString(w, plaintext)
	Expect(err).NotTo(HaveOccurred())
	Expect(w.Close()).To(Succeed())
	Expect(dst.Close()).To(Succeed())
	return out.Bytes()
}

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

func writeFile(path string, data []byte) {
	Expect(os.WriteFile(path, data, 0o600)).To(Succeed())
}

var _ = Describe("New", func() {
	It("returns a nil client when no provider is configured", func() {
		client, err := New(Config{})
		Expect(err).NotTo(HaveOccurred())
		Expect(client).To(BeNil())
	})

	It("returns an age client when a passphrase is configured", func() {
		client, err := New(Config{Age: AgeConfig{Passphrase: testPassphrase}})
		Expect(err).NotTo(HaveOccurred())
		Expect(client).To(BeAssignableToTypeOf(&ageClient{}))
	})
})

var _ = Describe("ageClient", func() {
	var (
		client Client
		dir    string
	)

	BeforeEach(func() {
		var err error
		client, err = New(Config{Age: AgeConfig{Passphrase: testPassphrase}})
		Expect(err).NotTo(HaveOccurred())
		dir = GinkgoT().TempDir()
	})

	DescribeTable("IsSecretFile",
		func(path string, expected bool) {
			Expect(client.IsSecretFile(path)).To(Equal(expected))
		},
		Entry("age file", "/stacks/app/secrets.age", true),
		Entry("dotenv file", "/stacks/app/.env", false),
		Entry("age in directory name only", "/stacks/x.age/file.txt", false),
		Entry("no extension", "age", false),
	)

	Describe("Decrypt", func() {
		It("returns empty results for a directory without age files", func() {
			writeFile(filepath.Join(dir, ".env"), []byte("A=1"))
			Expect(os.Mkdir(filepath.Join(dir, "nested.age"), 0o755)).To(Succeed())

			secrets, files, err := client.Decrypt(dir)
			Expect(err).NotTo(HaveOccurred())
			Expect(secrets).To(BeEmpty())
			Expect(files).To(BeEmpty())
		})

		It("decrypts binary and armored files and merges them in filename order", func() {
			writeFile(filepath.Join(dir, "b.age"), encryptEnv(testPassphrase, "SHARED=from-b\nB=2\n", true))
			writeFile(filepath.Join(dir, "a.age"), encryptEnv(testPassphrase, "SHARED=from-a\nA=1\n", false))

			secrets, files, err := client.Decrypt(dir)
			Expect(err).NotTo(HaveOccurred())
			Expect(files).To(Equal([]string{filepath.Join(dir, "a.age"), filepath.Join(dir, "b.age")}))
			Expect(secrets).To(HaveLen(3))
			Expect(*secrets["A"]).To(Equal("1"))
			Expect(*secrets["B"]).To(Equal("2"))
			Expect(*secrets["SHARED"]).To(Equal("from-b"))
		})

		It("fails for a missing directory", func() {
			_, _, err := client.Decrypt(filepath.Join(dir, "missing"))
			Expect(err).To(MatchError(os.ErrNotExist))
		})

		It("rejects symlinked age files", func() {
			target := filepath.Join(GinkgoT().TempDir(), "real.age")
			writeFile(target, encryptEnv(testPassphrase, "A=1", false))
			Expect(os.Symlink(target, filepath.Join(dir, "link.age"))).To(Succeed())

			_, _, err := client.Decrypt(dir)
			Expect(err).To(MatchError(ContainSubstring("must not be a symbolic link")))
		})

		It("fails when encrypted with a different passphrase", func() {
			writeFile(filepath.Join(dir, "s.age"), encryptEnv("other passphrase", "A=1", false))

			_, _, err := client.Decrypt(dir)
			Expect(err).To(MatchError(ContainSubstring("failed to decrypt age file")))
		})

		It("fails when the decrypted content is not valid dotenv", func() {
			writeFile(filepath.Join(dir, "s.age"), encryptEnv(testPassphrase, "A='unterminated", false))

			_, _, err := client.Decrypt(dir)
			Expect(err).To(MatchError(ContainSubstring("failed to parse decrypted dotenv")))
		})
	})
})
