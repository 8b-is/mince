package ccr

import (
	"bytes"
	"os"
	"testing"
)

func TestPutGetExactAndDisk(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("line one\nline two\nline three\n")
	id, err := s.Put(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 12 {
		t.Fatalf("id length %d", len(id))
	}
	got, err := s.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatal("round trip mismatch")
	}

	// Reopen from disk only.
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err = s2.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatal("disk round trip mismatch")
	}
	st, err := os.Stat(s2.path(id))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %o", st.Mode().Perm())
	}
}

func TestRetrieveFiltersAndWindows(t *testing.T) {
	s := NewMemory()
	raw := []byte("alpha 1\nbeta 2\nalpha 3\ngamma 4\nalpha 5\n")
	id, err := s.Put(raw)
	if err != nil {
		t.Fatal(err)
	}
	all, err := s.Retrieve(id, "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if all != string(raw) {
		t.Fatalf("all=%q", all)
	}
	alpha, err := s.Retrieve(id, "alpha", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if alpha != "alpha 3" {
		t.Fatalf("alpha=%q", alpha)
	}
	lit, err := s.Retrieve(id, "beta", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if lit != "beta 2" {
		t.Fatalf("lit=%q", lit)
	}
	if _, err := s.Get("doesnotexist"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
