package localsecrets

type Config struct {
	Age AgeConfig `embed:"" prefix:"age-" envprefix:"AGE_" group:"Age Options:"`
}

type Client interface {
	// Decrypt scans dir for secret files and returns merged values and file paths.
	Decrypt(dir string) (map[string]*string, []string, error)
	// IsSecretFile reports whether path belongs to this provider.
	IsSecretFile(path string) bool
}

// Provider returns the configured local secrets provider ("" when disabled).
func (c Config) Provider() string {
	if c.Age.configured() {
		return "age"
	}
	return ""
}

// New creates the configured local secrets client.
// It returns nil, nil when local secrets are disabled.
func New(cfg Config) (Client, error) {
	if !cfg.Age.configured() {
		return nil, nil
	}
	return newAgeClient(cfg.Age.Passphrase)
}
