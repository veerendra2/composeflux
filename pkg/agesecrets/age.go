package agesecrets

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"filippo.io/age"
	"filippo.io/age/armor"
	"github.com/compose-spec/compose-go/v2/dotenv"
)

type Config struct {
	Passphrase string `name:"age-passphrase" help:"Passphrase for age-encrypted secret files" env:"AGE_PASSPHRASE" default:"" group:"Age Options:"`
}

type Client struct {
	passphrase string
	identity   *age.ScryptIdentity
}

// New creates a new agesecrets client and precomputes the scrypt identity.
func New(passphrase string) *Client {
	var identity *age.ScryptIdentity
	if passphrase != "" {
		id, err := age.NewScryptIdentity(passphrase)
		if err == nil {
			identity = id
		}
	}
	return &Client{
		passphrase: passphrase,
		identity:   identity,
	}
}

// FindAgeFiles returns all *.age files located directly in dir (non-recursive), sorted by filename.
func (c *Client) FindAgeFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) == ".age" {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

// DecryptEnvFile decrypts an age-encrypted dotenv file (binary or armored) using the configured passphrase.
// Values are returned as map[string]*string compatible with Docker Compose types.MappingWithEquals.
func (c *Client) DecryptEnvFile(filePath string) (map[string]*string, error) {
	if c.passphrase == "" {
		return nil, fmt.Errorf("cannot decrypt %s: age passphrase is not set (use --age-passphrase or AGE_PASSPHRASE)", filePath)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read age file %s: %w", filePath, err)
	}

	identity := c.identity
	if identity == nil {
		id, err := age.NewScryptIdentity(c.passphrase)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize age scrypt identity: %w", err)
		}
		identity = id
	}

	// Support both ASCII-armored (age -a) and raw binary age formats
	var inReader io.Reader = bytes.NewReader(data)
	if bytes.Contains(data, []byte("-----BEGIN AGE ENCRYPTED FILE-----")) {
		inReader = armor.NewReader(bytes.NewReader(data))
	}

	decryptedReader, err := age.Decrypt(inReader, identity)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt age file %s: %w", filePath, err)
	}

	decryptedBytes, err := io.ReadAll(decryptedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read decrypted data from %s: %w", filePath, err)
	}

	envVars, err := dotenv.Parse(bytes.NewReader(decryptedBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to parse decrypted dotenv in %s: %w", filePath, err)
	}

	result := make(map[string]*string, len(envVars))
	for k, v := range envVars {
		val := v
		result[k] = &val
	}

	return result, nil
}
