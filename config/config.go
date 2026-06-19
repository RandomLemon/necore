package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"necore/util"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
)

const (
	configFileEnv = "NECORE_CONFIG_FILE"
	appConfigDir  = "necore"
	secretBytes   = 48
)

var (
	initOnce   sync.Once
	initErr    error
	configPath string
)

// Init finds or creates the .env file and loads it exactly once.
func Init() error {
	initOnce.Do(func() {
		configPath, initErr = locateOrCreateConfigFile()
		if initErr != nil {
			return
		}

		// Load does not overwrite variables already supplied by the process
		// environment. This lets deployment-time environment variables take
		// precedence over values stored in .env.
		if err := godotenv.Load(configPath); err != nil {
			initErr = fmt.Errorf("load config file %q: %w", configPath, err)
			return
		}

		setDefaultEnvironment("PORT", "3000")
		defaultSecret, _ := util.GenerateSecureToken("", 32)
		setDefaultEnvironment("SECRET", defaultSecret)
		setDefaultEnvironment("BOT_LOG_BUFFER_SIZE", "1000")

		secret := strings.TrimSpace(os.Getenv("SECRET"))
		if len(secret) < 1 {
			initErr = fmt.Errorf("SECRET must contain at least 1 characters, SECRET=%q", secret)
		}
	})

	return initErr
}

// Config keeps the existing project API. main should call Init before any
// server/database startup, so an error here indicates a programming error.
func Config(key string) string {
	if err := Init(); err != nil {
		panic(err)
	}
	return os.Getenv(key)
}

// Path returns the absolute path of the loaded .env file.
func Path() string {
	return configPath
}

func locateOrCreateConfigFile() (string, error) {
	if explicit := strings.TrimSpace(os.Getenv(configFileEnv)); explicit != "" {
		path, err := filepath.Abs(explicit)
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", configFileEnv, err)
		}
		if err := ensureConfigFile(path); err != nil {
			return "", err
		}
		return path, nil
	}

	existingCandidates, creationCandidates, err := defaultConfigCandidates()
	if err != nil {
		return "", err
	}

	for _, path := range existingCandidates {
		exists, err := regularFileExists(path)
		if err != nil {
			return "", err
		}
		if exists {
			return path, nil
		}
	}

	var creationErrors []error
	for _, path := range creationCandidates {
		if err := ensureConfigFile(path); err == nil {
			return path, nil
		} else {
			creationErrors = append(creationErrors, fmt.Errorf("%s: %w", path, err))
		}
	}

	return "", fmt.Errorf("unable to create configuration file: %w", errors.Join(creationErrors...))
}

func defaultConfigCandidates() ([]string, []string, error) {
	var existing []string
	var creation []string

	// Development compatibility: only trust the current working directory
	// when it looks like the project root. A production service may have an
	// unrelated or attacker-controlled working directory.
	if cwd, err := os.Getwd(); err == nil {
		goMod := filepath.Join(cwd, "go.mod")
		if ok, _ := regularFileExists(goMod); ok {
			path := filepath.Join(cwd, ".env")
			existing = append(existing, path)
			creation = append(creation, path)
		}
	}

	executable, err := os.Executable()
	if err != nil {
		return nil, nil, fmt.Errorf("locate executable: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	executablePath := filepath.Join(filepath.Dir(executable), ".env")
	existing = append(existing, executablePath)
	creation = append(creation, executablePath)

	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		return nil, nil, fmt.Errorf("locate user config directory: %w", err)
	}
	userConfigPath := filepath.Join(userConfigDir, appConfigDir, ".env")
	existing = append(existing, userConfigPath)
	creation = append(creation, userConfigPath)

	return uniquePaths(existing), uniquePaths(creation), nil
}

func ensureConfigFile(path string) error {
	exists, err := regularFileExists(path)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	lockPath := path + ".lock"
	deadline := time.Now().Add(5 * time.Second)

	for {
		lockFile, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return createConfigWhileLocked(path, lockPath, lockFile)
		}
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("create config lock: %w", err)
		}

		// Another process may have finished creating the file.
		exists, existsErr := regularFileExists(path)
		if existsErr != nil {
			return existsErr
		}
		if exists {
			return nil
		}

		// Recover from a process that died while holding the creation lock.
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > 30*time.Second {
			_ = os.Remove(lockPath)
			continue
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for config file creation")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func createConfigWhileLocked(path, lockPath string, lockFile *os.File) (returnErr error) {
	defer func() {
		_ = lockFile.Close()
		_ = os.Remove(lockPath)
	}()

	// Check again after acquiring the lock.
	exists, err := regularFileExists(path)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	secret, err := generateSecret()
	if err != nil {
		return err
	}

	content := fmt.Sprintf(
		"# Automatically generated by necore on first startup.\n"+
			"# Keep this file private. Changing SECRET invalidates existing JWTs.\n"+
			"PORT=3000\n"+
			"BOT_LOG_BUFFER_SIZE=2000\n"+
			"SECRET=%s\n",
		secret,
	)

	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".env.tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary config file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()

	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("set temporary config permissions: %w", err)
	}
	if _, err := temporary.WriteString(content); err != nil {
		return fmt.Errorf("write temporary config file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary config file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary config file: %w", err)
	}

	if err := os.Rename(temporaryPath, path); err != nil {
		// If another actor created the file despite not using our lock, prefer
		// the existing file rather than overwrite it.
		if exists, existsErr := regularFileExists(path); existsErr == nil && exists {
			return nil
		}
		return fmt.Errorf("publish config file: %w", err)
	}

	// On Unix this protects the secret. On Windows the directory ACL remains
	// the primary protection, but Chmod is still harmless.
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("set config file permissions: %w", err)
	}

	return nil
}

func generateSecret() (string, error) {
	buffer := make([]byte, secretBytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate SECRET: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func setDefaultEnvironment(key, value string) {
	if _, exists := os.LookupEnv(key); !exists {
		_ = os.Setenv(key, value)
	}
}

func regularFileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("%q exists but is not a regular file", path)
		}
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("inspect %q: %w", path, err)
}

func uniquePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	result := make([]string, 0, len(paths))

	for _, path := range paths {
		clean := filepath.Clean(path)
		if _, exists := seen[clean]; exists {
			continue
		}
		seen[clean] = struct{}{}
		result = append(result, clean)
	}
	return result
}
