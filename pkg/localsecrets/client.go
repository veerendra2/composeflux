package localsecrets

import "fmt"

type Config struct {
	Age AgeConfig `embed:"" prefix:"age-" envprefix:"AGE_" group:"Age Options:"`
}

type Client interface {
	// Decrypt scans dir for secret files and returns merged values and file paths.
	Decrypt(dir string) (map[string]*string, []string, error)
	// IsSecretFile reports whether path belongs to this provider.
	IsSecretFile(path string) bool
}

// Provider returns the configured local secrets provider.
func (c Config) Provider() (string, error) {
	if c.Age.configured() {
		return "age", nil
	}
	return "", nil
}

// New creates the configured local secrets client.
// It returns nil, nil when local secrets are disabled.
func New(cfg Config) (Client, error) {
	provider, err := cfg.Provider()
	if err != nil {
		return nil, err
	}

	switch provider {
	case "":
		return nil, nil
	case "age":
		return newAgeClient(cfg.Age.Passphrase)
	default:
		return nil, fmt.Errorf("unsupported local secrets provider: %s", provider)
	}
}
