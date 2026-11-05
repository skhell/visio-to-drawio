package main

import (
	"path/filepath"
	"testing"
)

func TestWithinRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("VISIODRAWIO_ROOT", root)
	if err := withinRoot(filepath.Join(root, "a", "b.vsdx")); err != nil {
		t.Errorf("inside root rejected: %v", err)
	}
	if err := withinRoot(filepath.Join(root, "..", "c.vsdx")); err == nil {
		t.Error("path outside root accepted")
	}
	t.Setenv("VISIODRAWIO_ROOT", "")
	if err := withinRoot("/anywhere"); err != nil {
		t.Errorf("unrestricted mode rejected path: %v", err)
	}
}

func TestCheckVSDX(t *testing.T) {
	for _, ok := range []string{"x.VSDX", "x.vstx"} {
		if err := checkVSDX(ok); err != nil {
			t.Error(err)
		}
	}
	if err := checkVSDX("x.vsd"); err != errLegacyVSD {
		t.Errorf("want errLegacyVSD, got %v", err)
	}
	if err := checkVSDX("x.txt"); err == nil {
		t.Error("txt accepted")
	}
}

func TestFileStem(t *testing.T) {
	if got := fileStem("My page #1"); got != "MY_PAGE_1" {
		t.Errorf("got %q", got)
	}
	if got := fileStem("***"); got != "PAGE" {
		t.Errorf("got %q", got)
	}
}
