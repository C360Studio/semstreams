package persona

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// LoadFromDirectory walks <root>/<role>/*.md and upserts each file
// into the persona manager as a fragment. Fragment ID is derived
// from the filename stem (e.g. "00-identity.md" -> "00-identity");
// role is derived from the immediate parent directory name.
//
// Source-of-truth semantics: files in this directory are the durable
// source of truth. Every call overwrites any KV entry whose fragment ID
// matches a file here — including entries written at runtime by tools
// such as update_persona. Runtime tool edits are ephemeral and reset to
// file state on the next restart. Call LoadFromDirectory at startup,
// after building the manager, before starting the KV watch.
//
// Missing directories and read errors are logged as warnings but do
// not fail boot. Startup-only; no watch — edits require restart.
func LoadFromDirectory(ctx context.Context, root string, mgr *Manager, logger *slog.Logger) error {
	if mgr == nil {
		return nil
	}
	return walkFragments(ctx, root, mgr.Upsert, logger)
}

// walkFragments is the testable core of LoadFromDirectory. It accepts a
// generic upsert func so unit tests can inject a fake without requiring NATS.
// The public API (LoadFromDirectory) delegates here; the internal helper is
// not exported to preserve the package's surface area.
func walkFragments(ctx context.Context, root string, upsert func(context.Context, *Persona) error, logger *slog.Logger) error {
	// Resolve root to an absolute clean path for symlink safety checks.
	absRoot, err := filepath.Abs(root)
	if err != nil {
		logger.Warn("persona file loader: could not resolve root path",
			slog.String("root", root),
			slog.Any("error", err))
		return nil
	}

	// A missing root is not a fatal condition — the directory is optional.
	if _, err := os.Stat(absRoot); os.IsNotExist(err) {
		logger.Warn("persona file loader: directory does not exist, skipping",
			slog.String("root", absRoot))
		return nil
	}

	var (
		loaded         int
		dirs           int
		seenDirs       = make(map[string]bool)
		firstUpsertErr error
	)

	// WalkDir does not follow symlinks by default, which is the primary
	// path-traversal protection. For completeness, depth-2 enforcement
	// (root → role → *.md) also bounds scope.
	walkErr := filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, walkErrIn error) error {
		// Honour context cancellation before doing any I/O.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}

		if walkErrIn != nil {
			// Report individual walk errors as warnings but continue.
			logger.Warn("persona file loader: walk error",
				slog.String("path", path),
				slog.Any("error", walkErrIn))
			return nil
		}

		// Skip the root itself.
		if path == absRoot {
			return nil
		}

		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			// filepath.Rel between two absolute paths rooted at absRoot
			// should never fail in practice; treat as a programming error
			// rather than silently skipping the entry.
			return fmt.Errorf("filepath.Rel: %w", err)
		}

		parts := strings.Split(rel, string(filepath.Separator))
		depth := len(parts)

		if d.IsDir() {
			if depth == 1 {
				// Role directory — allowed.
				return nil
			}
			// Nested directories are out of scope: skip the entire subtree.
			return filepath.SkipDir
		}

		// From here: regular file. Only depth-2 files are in scope.
		if depth != 2 {
			return nil
		}

		fileName := d.Name()

		// Skip hidden files.
		if strings.HasPrefix(fileName, ".") {
			return nil
		}

		// Skip non-markdown files.
		if !strings.HasSuffix(fileName, ".md") {
			return nil
		}

		// Symlink safety: verify the real path is inside absRoot.
		// WalkDir itself doesn't follow symlinks but a regular file entry
		// could be a symlink if the OS surfaces it with ModeSymlink set;
		// belt-and-suspenders check.
		if d.Type()&fs.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				logger.Warn("persona file loader: cannot resolve symlink, skipping",
					slog.String("path", path),
					slog.Any("error", err))
				return nil
			}
			if !strings.HasPrefix(resolved, absRoot+string(filepath.Separator)) {
				logger.Warn("persona file loader: symlink escapes root, skipping",
					slog.String("path", path),
					slog.String("resolved", resolved))
				return nil
			}
		}

		role := parts[0]
		fragID := strings.TrimSuffix(fileName, ".md")

		data, err := os.ReadFile(path)
		if err != nil {
			logger.Warn("persona file loader: could not read file, skipping",
				slog.String("path", path),
				slog.Any("error", err))
			return nil
		}

		p := &Persona{
			ID:      fragID,
			Content: string(data),
			Roles:   []string{role},
		}
		if upsertErr := upsert(ctx, p); upsertErr != nil {
			logger.Warn("persona file loader: upsert failed, skipping",
				slog.String("path", path),
				slog.String("role", role),
				slog.String("fragment_id", fragID),
				slog.Any("error", upsertErr))
			if firstUpsertErr == nil {
				firstUpsertErr = upsertErr
			}
			return nil
		}

		logger.Debug("persona file loader: loaded persona fragment",
			slog.String("role", role),
			slog.String("fragment_id", fragID),
			slog.Int("bytes", len(data)))

		if !seenDirs[role] {
			seenDirs[role] = true
			dirs++
		}
		loaded++
		return nil
	})

	if walkErr != nil && !errors.Is(walkErr, fs.ErrNotExist) {
		logger.Warn("persona file loader: walk failed",
			slog.String("root", absRoot),
			slog.Any("error", walkErr))
	}

	logFn := logger.Info
	if loaded == 0 {
		logFn = logger.Debug
	}
	logFn("persona file loader: load complete",
		slog.Int("fragments", loaded),
		slog.Int("role_dirs", dirs),
		slog.String("root", absRoot))

	return firstUpsertErr
}
