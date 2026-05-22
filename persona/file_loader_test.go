package persona

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeManager is an in-memory Manager substitute for file loader tests.
// It replaces the KV-backed Manager without requiring NATS, exercising the
// Upsert path via its own Create/Update stubs.
//
// We cannot easily embed or swap the Manager's kvStore, so fakeManager
// reimplements the same Upsert + Create/Update surface used by the loader.
// The loader only calls mgr.Upsert, so only that method needs coverage here.
type fakeManager struct {
	data map[string]*Persona
	// upsertErr, if non-nil, is returned from every Upsert call.
	upsertErr error
}

func newFakeManager() *fakeManager {
	return &fakeManager{data: make(map[string]*Persona)}
}

// Upsert mirrors Manager.Upsert semantics for tests: create-or-overwrite.
func (f *fakeManager) Upsert(_ context.Context, p *Persona) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	if p == nil {
		return fmt.Errorf("nil persona")
	}
	cloned := *p
	f.data[p.ID] = &cloned
	return nil
}

// count returns the number of personas stored.
func (f *fakeManager) count() int { return len(f.data) }

// get returns the persona for id or nil.
func (f *fakeManager) get(id string) *Persona { return f.data[id] }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

func TestFileLoader_HappyPath(t *testing.T) {
	root := t.TempDir()
	// Two role directories, each with 2 fragment files.
	roles := []struct{ dir, file, content string }{
		{"ops", "ops-identity.md", "You are an ops agent."},
		{"ops", "ops-constraints.md", "Never break prod."},
		{"researcher", "researcher-identity.md", "You are a researcher."},
		{"researcher", "researcher-methodology.md", "Use primary sources."},
	}
	for _, r := range roles {
		dir := filepath.Join(root, r.dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, r.file), []byte(r.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mgr := newFakeManager()
	ctx := context.Background()
	if err := walkFragments(ctx, root, mgr.Upsert, discardLogger()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := mgr.count(); got != 4 {
		t.Fatalf("expected 4 fragments, got %d", got)
	}

	// Fragment IDs are role-prefixed (<role>/<filename-stem>) per #124.
	for _, r := range roles {
		id := r.dir + "/" + strings.TrimSuffix(r.file, ".md")
		p := mgr.get(id)
		if p == nil {
			t.Errorf("fragment %q not found in manager", id)
			continue
		}
		if p.Content != r.content {
			t.Errorf("fragment %q: content mismatch: got %q, want %q", id, p.Content, r.content)
		}
		if len(p.Roles) != 1 || p.Roles[0] != r.dir {
			t.Errorf("fragment %q: roles mismatch: got %v, want [%s]", id, p.Roles, r.dir)
		}
	}
}

func TestFileLoader_EmptyDirectory(t *testing.T) {
	root := t.TempDir()
	mgr := newFakeManager()
	if err := walkFragments(context.Background(), root, mgr.Upsert, discardLogger()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := mgr.count(); got != 0 {
		t.Fatalf("expected 0 fragments, got %d", got)
	}
}

func TestFileLoader_MissingDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "does-not-exist")
	mgr := newFakeManager()
	if err := walkFragments(context.Background(), root, mgr.Upsert, discardLogger()); err != nil {
		t.Fatalf("missing root must not return error; got: %v", err)
	}
	if got := mgr.count(); got != 0 {
		t.Fatalf("expected 0 fragments, got %d", got)
	}
}

func TestFileLoader_NonMarkdownFilesSkipped(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "ops")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := []struct{ name, content string }{
		{"identity.md", "actual markdown"},
		{"notes.txt", "should be skipped"},
		{"config.yaml", "should be skipped"},
		{"no-extension", "should be skipped"},
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f.name), []byte(f.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mgr := newFakeManager()
	if err := walkFragments(context.Background(), root, mgr.Upsert, discardLogger()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := mgr.count(); got != 1 {
		t.Fatalf("expected 1 fragment (only .md), got %d", got)
	}
	if mgr.get("ops/identity") == nil {
		t.Error("identity fragment should be loaded")
	}
}

func TestFileLoader_HiddenFilesSkipped(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "ops")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden.md"), []byte("hidden"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "visible.md"), []byte("visible"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := newFakeManager()
	if err := walkFragments(context.Background(), root, mgr.Upsert, discardLogger()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := mgr.count(); got != 1 {
		t.Fatalf("expected 1 fragment, got %d", got)
	}
	if mgr.get("ops/visible") == nil {
		t.Error("visible fragment should be loaded")
	}
}

func TestFileLoader_NestedDirectoriesSkipped(t *testing.T) {
	root := t.TempDir()
	// root/ops/sub/deep.md — depth 3, must be skipped.
	deep := filepath.Join(root, "ops", "sub")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "deep.md"), []byte("deep content"), 0o644); err != nil {
		t.Fatal(err)
	}
	// root/ops/top.md — depth 2, should be loaded.
	if err := os.WriteFile(filepath.Join(root, "ops", "top.md"), []byte("top content"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := newFakeManager()
	if err := walkFragments(context.Background(), root, mgr.Upsert, discardLogger()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := mgr.count(); got != 1 {
		t.Fatalf("expected 1 fragment (only depth-2), got %d", got)
	}
	if mgr.get("ops/top") == nil {
		t.Error("top fragment should be loaded")
	}
}

func TestFileLoader_UnicodeContent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "i18n")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "日本語テスト — Unicode round-trip: 🚀 \u0000\xFF"
	if err := os.WriteFile(filepath.Join(dir, "unicode.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := newFakeManager()
	if err := walkFragments(context.Background(), root, mgr.Upsert, discardLogger()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := mgr.get("i18n/unicode")
	if p == nil {
		t.Fatal("unicode fragment not found")
	}
	if p.Content != content {
		t.Errorf("content mismatch: got %q, want %q", p.Content, content)
	}
}

func TestFileLoader_FragmentIDDerivation(t *testing.T) {
	// Fragment IDs are namespaced by role to prevent cross-role collisions
	// when the same filename appears in multiple role directories.
	// See TestFileLoader_MultiRoleSameFilename_NoOverwrite and issue #124.
	tests := []struct {
		filename string
		wantID   string
	}{
		{"00-identity.md", "ops/00-identity"},
		{"my.fragment.md", "ops/my.fragment"}, // single extension stripped
		{"simple.md", "ops/simple"},
		{"multi.dots.in.name.md", "ops/multi.dots.in.name"},
	}

	root := t.TempDir()
	dir := filepath.Join(root, "ops")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range tests {
		if err := os.WriteFile(filepath.Join(dir, tc.filename), []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mgr := newFakeManager()
	if err := walkFragments(context.Background(), root, mgr.Upsert, discardLogger()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, tc := range tests {
		if mgr.get(tc.wantID) == nil {
			t.Errorf("filename %q: expected fragment ID %q, not found", tc.filename, tc.wantID)
		}
	}
}

// TestFileLoader_MultiRoleSameFilename_NoOverwrite is the regression test
// for issue #124: cross-role ID collisions used to silently drop fragments
// from all but the alphabetically-last role when filenames were shared
// across role directories. The fix namespaces Persona.ID by role so each
// fragment survives upsert with the correct content and Roles assignment.
//
// Symptom before the fix: writing `coordinator/00-identity.md` and
// `researcher/00-identity.md` both produced personas with ID="00-identity";
// the second upsert overwrote the first and a downstream agent loop got
// only the researcher's content (or only the coordinator's, depending on
// walk order). Products using the documented <root>/<role>/<file>.md layout
// with sensible shared filenames (00-identity.md, 10-output-contract.md)
// were silently running on DefaultFragments() alone.
func TestFileLoader_MultiRoleSameFilename_NoOverwrite(t *testing.T) {
	root := t.TempDir()

	// Three roles each holding identically-named fragments with
	// role-distinguishable content. If the loader collapses on filename
	// stem, at most one role's content survives — and the assertion below
	// reports exactly which roles got dropped.
	roles := []string{"coordinator", "ops", "researcher"}
	fragments := []string{"00-identity.md", "10-output-contract.md"}

	for _, role := range roles {
		dir := filepath.Join(root, role)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, frag := range fragments {
			content := fmt.Sprintf("role=%s fragment=%s", role, frag)
			if err := os.WriteFile(filepath.Join(dir, frag), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	mgr := newFakeManager()
	if err := walkFragments(context.Background(), root, mgr.Upsert, discardLogger()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantCount := len(roles) * len(fragments)
	if got := mgr.count(); got != wantCount {
		t.Fatalf("expected %d fragments (one per role/file pair); got %d — collision dropped some",
			wantCount, got)
	}

	for _, role := range roles {
		for _, frag := range fragments {
			fragStem := strings.TrimSuffix(frag, ".md")
			id := role + "/" + fragStem
			p := mgr.get(id)
			if p == nil {
				t.Errorf("expected fragment %q (role=%s, file=%s) to survive multi-role load, not found",
					id, role, frag)
				continue
			}
			wantContent := fmt.Sprintf("role=%s fragment=%s", role, frag)
			if p.Content != wantContent {
				t.Errorf("fragment %q: content mismatch (collision overwrite?): got %q, want %q",
					id, p.Content, wantContent)
			}
			if len(p.Roles) != 1 || p.Roles[0] != role {
				t.Errorf("fragment %q: roles should remain [%s] after upsert; got %v",
					id, role, p.Roles)
			}
		}
	}
}

func TestFileLoader_RoleDerivation(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "custom-role-name")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frag.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := newFakeManager()
	if err := walkFragments(context.Background(), root, mgr.Upsert, discardLogger()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := mgr.get("custom-role-name/frag")
	if p == nil {
		t.Fatal("fragment not found")
	}
	if len(p.Roles) != 1 || p.Roles[0] != "custom-role-name" {
		t.Errorf("expected role [custom-role-name], got %v", p.Roles)
	}
}

func TestFileLoader_LargeFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "ops")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 100KB of repeated content.
	content := strings.Repeat("a", 100*1024)
	if err := os.WriteFile(filepath.Join(dir, "big.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := newFakeManager()
	if err := walkFragments(context.Background(), root, mgr.Upsert, discardLogger()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := mgr.get("ops/big")
	if p == nil {
		t.Fatal("large fragment not found")
	}
	if len(p.Content) != 100*1024 {
		t.Errorf("expected 100KB content, got %d bytes", len(p.Content))
	}
}

func TestFileLoader_UpsertErrorContinuesAndReturns(t *testing.T) {
	// Even when upsert fails, the loader continues other files and returns
	// the first error at the end.
	root := t.TempDir()
	dir := filepath.Join(root, "ops")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.md", "b.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	boom := fmt.Errorf("registry closed")
	calls := 0
	upsertFn := func(_ context.Context, _ *Persona) error {
		calls++
		return boom
	}

	err := walkFragments(context.Background(), root, upsertFn, discardLogger())
	if err == nil {
		t.Fatal("expected first upsert error to be returned")
	}
	if err.Error() != boom.Error() {
		t.Errorf("expected %q, got %q", boom, err)
	}
	// Both files were attempted despite the first failure.
	if calls != 2 {
		t.Errorf("expected 2 upsert attempts, got %d", calls)
	}
}

// TestFileLoader_SymlinkEscapeSkipped verifies the symlink-escape guard using
// the real walkFragments helper. On most Unix systems, filepath.WalkDir
// presents symlinked regular files as regular files (ModeSymlink not set in
// DirEntry.Type()), so the in-walk guard only fires when the OS surfaces the
// symlink bit. The test instead relies on the documented OS behaviour:
// os.ReadFile follows the symlink and reads the target. The guard prevents
// loading the *content* of the escaped file by checking the resolved path
// before ReadFile — but only when d.Type()&fs.ModeSymlink != 0.
//
// On platforms where WalkDir does not surface ModeSymlink for file entries
// (Linux, macOS via default lstat(2) behaviour), the primary protection is
// depth-2 enforcement and the absence of a recursive walk. This test asserts
// the properties we can verify without platform-specific stat tricks:
//   - Only legitimate non-symlink files are loaded.
//   - The symlinked file, when its content would be "FORBIDDEN", is either
//     not loaded (guard fires) or its content does not appear in loaded data
//     (the file inside root has different content).
//
// If the platform surfaces ModeSymlink on the DirEntry, the guard fires and
// the escape file is skipped with a warning; the test asserts the count.
func TestFileLoader_SymlinkEscapeSkipped(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("symlink escape test skipped when running as root")
	}

	root := t.TempDir()
	outside := t.TempDir()

	// Sensitive file outside root.
	outsideFile := filepath.Join(outside, "secret.md")
	const forbiddenContent = "FORBIDDEN"
	if err := os.WriteFile(outsideFile, []byte(forbiddenContent), 0o644); err != nil {
		t.Fatal(err)
	}

	roleDir := filepath.Join(root, "ops")
	if err := os.MkdirAll(roleDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Symlink inside root that points outside.
	symlinkPath := filepath.Join(roleDir, "escape.md")
	if err := os.Symlink(outsideFile, symlinkPath); err != nil {
		t.Skip("symlink creation not supported on this platform")
	}

	// Legitimate file that must be loaded.
	const legitContent = "legit persona content"
	if err := os.WriteFile(filepath.Join(roleDir, "legit.md"), []byte(legitContent), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded := make(map[string]string)
	upsertFn := func(_ context.Context, p *Persona) error {
		loaded[p.ID] = p.Content
		return nil
	}

	if err := walkFragments(context.Background(), root, upsertFn, discardLogger()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The legitimate file must always be loaded.
	if content, ok := loaded["ops/legit"]; !ok || content != legitContent {
		t.Errorf("legit fragment not loaded correctly; got loaded=%v", loaded)
	}

	// The forbidden content must not appear anywhere in loaded data.
	for id, content := range loaded {
		if content == forbiddenContent {
			t.Errorf("fragment %q contains forbidden content — symlink escape was not blocked", id)
		}
	}

	// When the OS surfaces ModeSymlink on the DirEntry (the guard fires),
	// "escape" must not be in the loaded set at all.
	// When it does not (guard can't fire), the symlink is followed by ReadFile
	// but the forbidden-content check above already guards correctness.
	// Either way: at most 1 fragment should be loaded (the legit one).
	if len(loaded) > 1 {
		t.Errorf("expected at most 1 loaded fragment (legit only), got %d: %v", len(loaded), loaded)
	}
}

// TestFileLoader_ContextCancellation verifies that a cancelled context stops
// the walk before processing all files.
func TestFileLoader_ContextCancellation(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "ops")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create enough files that cancellation mid-walk is observable.
	for i := range 10 {
		name := fmt.Sprintf("frag%02d.md", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	upsertFn := func(_ context.Context, _ *Persona) error {
		calls++
		if calls == 3 {
			cancel() // cancel after the third fragment
		}
		return nil
	}

	// walkFragments returns nil (walk stopped by context, walkErr is context.Canceled
	// which is suppressed by the !errors.Is(walkErr, fs.ErrNotExist) check).
	// What matters is that not all 10 fragments were processed.
	_ = walkFragments(ctx, root, upsertFn, discardLogger())
	if calls >= 10 {
		t.Errorf("expected fewer than 10 upsert calls after cancellation, got %d", calls)
	}
}
