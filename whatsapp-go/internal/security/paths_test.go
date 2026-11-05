package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateSendPath(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "sub", "a.txt")
	_ = os.MkdirAll(filepath.Dir(inside), 0o700)
	_ = os.WriteFile(inside, []byte("x"), 0o600)
	outside := filepath.Join(t.TempDir(), "b.txt")
	_ = os.WriteFile(outside, []byte("x"), 0o600)
	link := filepath.Join(root, "link.txt")
	_ = os.Symlink(outside, link)

	if got, err := ValidateSendPath(inside, root); err != nil || filepath.Base(got) != "a.txt" {
		t.Errorf("inside: %v %q", err, got)
	}
	if _, err := ValidateSendPath(outside, root); err == nil {
		t.Error("outside path accepted")
	}
	if _, err := ValidateSendPath(link, root); err == nil {
		t.Error("symlink escaping the root accepted")
	}
	if _, err := ValidateSendPath(filepath.Join(root, "sub"), root); err == nil {
		t.Error("directory accepted")
	}
	if _, err := ValidateSendPath(filepath.Join(root, "missing.txt"), root); err == nil {
		t.Error("missing file accepted")
	}
	if _, err := ValidateSendPath("", root); err == nil {
		t.Error("empty path accepted")
	}
}

func TestSafeFilename(t *testing.T) {
	if SafeFilename("../../etc/passwd") != "passwd" || SafeFilename("..") == ".." || SafeFilename("") == "" {
		t.Error("SafeFilename")
	}
	if SafeDirName("1234@s.whatsapp.net") != "1234@s.whatsapp.net" || SafeDirName("a/b:c") != "a_b_c" {
		t.Error("SafeDirName")
	}
}
