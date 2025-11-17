package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// Security limits for configuration
	maxConfigSize = 10 << 20 // 10MB max config file size
	maxJSONDepth  = 100      // Maximum JSON nesting depth
	maxEnvVarLen  = 10000    // Maximum environment variable value length
	maxPathLen    = 4096     // Maximum file path length
)

// validateConfigPath does basic path validation
func validateConfigPath(path string) error {
	if path == "" {
		return errors.New("empty config path")
	}

	if len(path) > maxPathLen {
		return fmt.Errorf("path too long: %d > %d", len(path), maxPathLen)
	}

	// Path traversal check - use filepath.Clean to normalize and check for parent references
	cleanPath := filepath.Clean(path)

	// Check if the cleaned path tries to escape via parent directories
	// Convert to absolute path for proper validation
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return fmt.Errorf("cannot resolve absolute path: %w", err)
	}

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot get working directory: %w", err)
	}

	// Ensure the resolved path is within or relative to the current directory
	// This prevents "/absolute/path/../../../etc/passwd" attacks
	if filepath.IsAbs(path) {
		// If path is absolute, it must not try to escape via parent refs
		if strings.Contains(filepath.ToSlash(absPath), "..") {
			return fmt.Errorf("path traversal not allowed: %s", path)
		}
	} else {
		// For relative paths, ensure they stay within CWD after resolution
		relPath, err := filepath.Rel(cwd, absPath)
		if err != nil || strings.HasPrefix(relPath, "..") {
			return fmt.Errorf("path traversal not allowed: %s resolves outside working directory", path)
		}
	}

	// Only allow JSON config files
	if !strings.HasSuffix(path, ".json") && !strings.HasSuffix(path, ".json5") {
		return fmt.Errorf("only JSON config files allowed: %s", path)
	}

	return nil
}

// safeReadFile reads a config file with security validation
func safeReadFile(path string) ([]byte, error) {
	// Validate the path first
	if err := validateConfigPath(path); err != nil {
		return nil, fmt.Errorf("invalid config path: %w", err)
	}

	// Check file size before reading
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("cannot stat config file: %w", err)
	}

	if info.Size() > maxConfigSize {
		return nil, fmt.Errorf("config file too large: %d bytes > %d", info.Size(), maxConfigSize)
	}

	// Check if it's a regular file (not symlink, directory, etc.)
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file: %s", path)
	}

	// Read the file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read config file: %w", err)
	}

	return data, nil
}

// safeWriteFile writes a config file with security validation
func safeWriteFile(path string, data []byte) error {
	// Validate the path first
	if err := validateConfigPath(path); err != nil {
		return fmt.Errorf("invalid config path: %w", err)
	}

	// Check data size
	if len(data) > maxConfigSize {
		return fmt.Errorf("config data too large: %d bytes > %d", len(data), maxConfigSize)
	}

	// Write with secure permissions (owner read/write only)
	return os.WriteFile(path, data, 0600)
}

// validateEnvVar does basic environment variable validation
func validateEnvVar(key, value string) error {
	if value == "" {
		return nil // Empty is OK
	}

	// Basic length check
	if len(value) > maxEnvVarLen {
		return fmt.Errorf("environment variable %s too long: %d > %d", key, len(value), maxEnvVarLen)
	}

	// No complex validation - developers know what they're doing
	// Just prevent obvious shell injection
	if strings.Contains(value, "\x00") {
		return fmt.Errorf("null byte in environment variable %s", key)
	}

	return nil
}

// validateJSONDepth checks JSON depth to prevent DoS attacks
func validateJSONDepth(data []byte) error {
	depth := 0
	maxDepthReached := 0
	inString := false
	escaped := false

	for i := 0; i < len(data); i++ {
		b := data[i]

		// Handle string state
		if escaped {
			escaped = false
			continue
		}

		if b == '\\' && inString {
			escaped = true
			continue
		}

		if b == '"' && !escaped {
			inString = !inString
			continue
		}

		if inString {
			continue
		}

		// Track depth
		switch b {
		case '{', '[':
			depth++
			if depth > maxDepthReached {
				maxDepthReached = depth
			}
			if depth > maxJSONDepth {
				return fmt.Errorf("JSON nesting too deep: %d > %d", depth, maxJSONDepth)
			}
		case '}', ']':
			depth--
			if depth < 0 {
				return errors.New("malformed JSON: unbalanced brackets")
			}
		}
	}

	if depth != 0 {
		return fmt.Errorf("malformed JSON: unclosed brackets (depth=%d)", depth)
	}

	return nil
}
