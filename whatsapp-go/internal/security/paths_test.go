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
	// Creating symlinks needs a privilege on Windows; skip that case there.
	haveLink := os.Symlink(outside, link) == nil

	if got, err := ValidateSendPath(inside, root); err != nil || filepath.Base(got) != "a.txt" {
		t.Errorf("inside: %v %q", err, got)
	}
	if _, err := ValidateSendPath(outside, root); err == nil {
		t.Error("outside path accepted")
	}
	if _, err := ValidateSendPath(link, root); haveLink && err == nil {
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

func TestSafeFilenameWindowsRules(t *testing.T) {
	for in, want := range map[string]string{
		`facture: mars?.pdf`: "facture_ mars_.pdf",
		"notes.":             "notes",
		"CON.txt":            "_CON.txt",
		"lpt1":               "_lpt1",
		"console.txt":        "console.txt",
		"a<b>|c*.png":        "a_b__c_.png",
	} {
		if got := SafeFilename(in); got != want {
			t.Errorf("SafeFilename(%q) = %q, want %q", in, got, want)
		}
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
