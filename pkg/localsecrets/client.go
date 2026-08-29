package localsecrets

type Config struct {
	Passphrase string `name:"age-passphrase" help:"Passphrase for age-encrypted secret files" env:"AGE_PASSPHRASE" default:"" group:"Age Options:"`
}

type Client interface {
	// Decrypt scans dir for secret files and returns merged values and file paths.
	Decrypt(dir string) (map[string]*string, []string, error)
}

// New creates a local secrets client.
func New(cfg Config) Client {
	return newAgeClient(cfg.Passphrase)
}
