package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRequest(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantFile string
		wantMode string
		wantErr  bool
	}{
		{name: "plain", body: "file.txt\x00octet\x00", wantFile: "file.txt", wantMode: "octet"},
		{name: "mode lowercased", body: "file.txt\x00OCTET\x00", wantFile: "file.txt", wantMode: "octet"},
		{name: "empty filename", body: "\x00octet\x00", wantErr: true},
		{name: "missing mode", body: "file.txt", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, mode, err := parseRequest([]byte(tt.body))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if file != tt.wantFile || mode != tt.wantMode {
				t.Fatalf("got (%q,%q), want (%q,%q)", file, mode, tt.wantFile, tt.wantMode)
			}
		})
	}
}

func TestRelName(t *testing.T) {
	sep := string(filepath.Separator)
	cases := map[string]string{
		"file.txt":         "file.txt",
		"sub/file.txt":     filepath.Join("sub", "file.txt"),
		"/abs.txt":         "abs.txt",
		"./x":              "x",
		"../etc/passwd":    filepath.Join("etc", "passwd"),
		"../../etc/passwd": filepath.Join("etc", "passwd"),
		"sub/../../escape": "escape",
	}
	for in, want := range cases {
		if got := relName(in); got != want {
			t.Errorf("relName(%q)=%q, want %q", in, got, want)
		}
		// Result must never be absolute or start with a traversal component.
		if got := relName(in); strings.HasPrefix(got, sep) || strings.HasPrefix(got, "..") {
			t.Errorf("relName(%q)=%q is not safely relative", in, got)
		}
	}
}

// TestRootSymlinkEscape is a regression test for the symlink-escape hole: a
// symlink inside the served root that points outside it must not be followed.
func TestRootSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	secret := filepath.Join(base, "secret.txt")
	if err := os.MkdirAll(root, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secret, []byte("top secret"), 0600); err != nil {
		t.Fatal(err)
	}
	// A symlink inside root pointing at the out-of-tree secret.
	if err := os.Symlink(secret, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	r, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()

	if f, err := r.Open("link"); err == nil {
		_ = f.Close()
		t.Fatal("symlink escape was NOT blocked: opened out-of-root target")
	}
	if f, err := r.Open(relName("../secret.txt")); err == nil {
		_ = f.Close()
		t.Fatal("traversal escape was NOT blocked")
	}
}

func TestBuildData(t *testing.T) {
	p := buildData(7, []byte("hello"))
	if binary.BigEndian.Uint16(p[:2]) != opDATA {
		t.Errorf("opcode = %d, want %d", binary.BigEndian.Uint16(p[:2]), opDATA)
	}
	if binary.BigEndian.Uint16(p[2:4]) != 7 {
		t.Errorf("block = %d, want 7", binary.BigEndian.Uint16(p[2:4]))
	}
	if string(p[4:]) != "hello" {
		t.Errorf("payload = %q, want %q", p[4:], "hello")
	}
}

func TestBuildACK(t *testing.T) {
	p := buildACK(9)
	if binary.BigEndian.Uint16(p[:2]) != opACK || binary.BigEndian.Uint16(p[2:4]) != 9 {
		t.Fatalf("got % x, want ACK block 9", p)
	}
}

// FuzzParseRequest throws arbitrary bytes at the RRQ/WRQ parser; it must never
// panic, and a successful parse must honor the documented invariants.
func FuzzParseRequest(f *testing.F) {
	f.Add([]byte("file.txt\x00octet\x00"))
	f.Add([]byte("f\x00NETASCII\x00blksize\x00512\x00"))
	f.Add([]byte("\x00octet\x00"))
	f.Add([]byte("nonull"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		name, mode, err := parseRequest(b)
		if err != nil {
			return
		}
		if name == "" {
			t.Errorf("parseRequest(%q) returned empty filename with no error", b)
		}
		if mode != strings.ToLower(mode) {
			t.Errorf("parseRequest(%q) mode %q is not lowercased", b, mode)
		}
	})
}

// FuzzRelName asserts the traversal guard's core invariant for any input: the
// result is never absolute and never escapes upward with a "..".
func FuzzRelName(f *testing.F) {
	f.Add("file.txt")
	f.Add("../../etc/passwd")
	f.Add("/abs/path")
	f.Add("a/../../b")
	f.Add("")
	sep := string(os.PathSeparator)
	f.Fuzz(func(t *testing.T, name string) {
		got := relName(name)
		if strings.HasPrefix(got, sep) {
			t.Errorf("relName(%q)=%q is absolute", name, got)
		}
		if got == ".." || strings.HasPrefix(got, ".."+sep) {
			t.Errorf("relName(%q)=%q escapes upward", name, got)
		}
	})
}

func TestResolveVersion(t *testing.T) {
	orig := version
	defer func() { version = orig }()
	version = "v9.9.9"
	if got := resolveVersion(); got != "v9.9.9" {
		t.Errorf("resolveVersion() = %q, want v9.9.9", got)
	}
}
