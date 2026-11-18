package devboximage

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestWriteContext(t *testing.T) {
	dir := t.TempDir()
	if err := WriteContext(dir); err != nil {
		t.Fatal(err)
	}

	df, err := os.ReadFile(filepath.Join(dir, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile("Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if string(df) != string(src) {
		t.Error("書き出した Dockerfile が埋め込み元と一致しない")
	}
	// devbox-check をイメージに組み込む行がある（ベースイメージ名には依存しない）
	if !strings.Contains(string(df), "COPY devbox-check /usr/local/bin/devbox-check") {
		t.Error("Dockerfile の中身が想定と違う")
	}

	st, err := os.Stat(filepath.Join(dir, "devbox-check"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o755 {
		t.Errorf("devbox-check のモード = %o, want 755", st.Mode().Perm())
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "Dockerfile,devbox-check" {
		t.Errorf("書き出されたファイル = %v, want [Dockerfile devbox-check]", names)
	}
}
