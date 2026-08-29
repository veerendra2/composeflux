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

type ageClient struct {
	passphrase string
	identity   *age.ScryptIdentity
}

// newAgeClient prepares the reusable age identity when a passphrase is configured.
func newAgeClient(passphrase string) *ageClient {
	var identity *age.ScryptIdentity
	if passphrase != "" {
		identity, _ = age.NewScryptIdentity(passphrase)
	}
	return &ageClient{passphrase: passphrase, identity: identity}
}

// Decrypt scans dir for age-encrypted dotenv files and returns their merged values and paths.
func (c *ageClient) Decrypt(dir string) (map[string]*string, []string, error) {
	files, err := findFiles(dir)
	if err != nil {
		return nil, nil, err
	}

	result := make(map[string]*string)
	for _, file := range files {
		envs, err := c.decryptEnvFile(file)
		if err != nil {
			return nil, nil, err
		}
		maps.Copy(result, envs)
	}

	return result, files, nil
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
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Strings(files)

	return files, nil
}

// decryptEnvFile decrypts an age-encrypted dotenv file in binary or armored format.
func (c *ageClient) decryptEnvFile(filePath string) (map[string]*string, error) {
	if c.passphrase == "" {
		return nil, fmt.Errorf("cannot decrypt %s: age passphrase is not set (use --age-passphrase or AGE_PASSPHRASE)", filePath)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read age file %s: %w", filePath, err)
	}

	var inReader io.Reader = bytes.NewReader(data)
	if bytes.Contains(data, []byte("-----BEGIN AGE ENCRYPTED FILE-----")) {
		inReader = armor.NewReader(bytes.NewReader(data))
	}

	identity := c.identity
	if identity == nil {
		identity, err = age.NewScryptIdentity(c.passphrase)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize age scrypt identity: %w", err)
		}
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
	for key, value := range envVars {
		value := value
		result[key] = &value
	}

	return result, nil
}
