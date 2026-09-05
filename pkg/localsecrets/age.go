package localsecrets

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"sort"

	"filippo.io/age"
	"filippo.io/age/armor"
	"github.com/compose-spec/compose-go/v2/dotenv"
)

type AgeConfig struct {
	Passphrase string `name:"passphrase" help:"Passphrase for age-encrypted secret files" env:"PASSPHRASE" xor:"local-provider"`
}

// configured reports whether Age local secrets are enabled.
func (c AgeConfig) configured() bool {
	return c.Passphrase != ""
}

type ageClient struct {
	identity *age.ScryptIdentity
}

// newAgeClient prepares the reusable age identity.
func newAgeClient(passphrase string) (*ageClient, error) {
	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize age scrypt identity: %w", err)
	}
	return &ageClient{identity: identity}, nil
}

// Decrypt scans dir for age-encrypted dotenv files and returns their merged values and paths.
func (c *ageClient) Decrypt(dir string) (map[string]*string, []string, error) {
	files, err := findFiles(dir)
	if err != nil {
		return nil, nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = root.Close() }()

	result := make(map[string]*string)
	for _, file := range files {
		data, err := root.ReadFile(filepath.Base(file))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read age file %s: %w", file, err)
		}
		envs, err := c.decryptEnvFile(file, data)
		if err != nil {
			return nil, nil, err
		}
		maps.Copy(result, envs)
	}

	return result, files, nil
}

// IsSecretFile reports whether path is an Age-encrypted secret file.
func (*ageClient) IsSecretFile(path string) bool {
	return filepath.Ext(path) == ".age"
}

// findFiles returns all *.age files located directly in dir, sorted by filename.
func findFiles(dir string) ([]string, error) {
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
			if entry.Type()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("age secret file must not be a symbolic link: %s", filepath.Join(dir, entry.Name()))
			}
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Strings(files)

	return files, nil
}

// decryptEnvFile decrypts age-encrypted dotenv data in binary or armored format.
func (c *ageClient) decryptEnvFile(filePath string, data []byte) (map[string]*string, error) {
	var inReader io.Reader = bytes.NewReader(data)
	if bytes.Contains(data, []byte("-----BEGIN AGE ENCRYPTED FILE-----")) {
		inReader = armor.NewReader(bytes.NewReader(data))
	}

	decryptedReader, err := age.Decrypt(inReader, c.identity)
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
	for key, value := range envVars {
		value := value
		result[key] = &value
	}

	return result, nil
}
