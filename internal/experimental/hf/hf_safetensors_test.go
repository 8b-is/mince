package hf

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"mince/internal/compression/ternary"
)

func writeFixture(t *testing.T, pos, neg uint64, bias int64) (string, string) {
	t.Helper()
	header := `{"trit_pos":{"dtype":"U64","shape":[1],"data_offsets":[0,8]},"trit_neg":{"dtype":"U64","shape":[1],"data_offsets":[8,16]},"bias":{"dtype":"I64","shape":[1],"data_offsets":[16,24]}}`
	buf := make([]byte, 8+len(header)+24)
	binary.LittleEndian.PutUint64(buf[:8], uint64(len(header)))
	copy(buf[8:], header)
	data := buf[8+len(header):]
	binary.LittleEndian.PutUint64(data[0:], pos)
	binary.LittleEndian.PutUint64(data[8:], neg)
	binary.LittleEndian.PutUint64(data[16:], uint64(bias))
	path := filepath.Join(t.TempDir(), "kompress.safetensors")
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf)
	return path, hex.EncodeToString(sum[:])
}

func TestLoadKompressUltra(t *testing.T) {
	path, sum := writeFixture(t, 0b101, 0b010, 3)
	k, err := LoadKompressUltraSafetensors(path, sum)
	if err != nil {
		t.Fatal(err)
	}
	if k.Pos != 0b101 || k.Neg != 0b010 || k.Bias != 3 {
		t.Fatalf("bad head: %+v", k)
	}
	h := ternary.TritPlane64{Pos: 0b101, Neg: 0b010}
	if got := k.ScoreSimHash64(h).String(); got != "3/32" {
		t.Fatalf("score %s want 3/32", got)
	}
}

func TestRejectsBadSHA(t *testing.T) {
	path, _ := writeFixture(t, 1, 2, 0)
	if _, err := LoadKompressUltraSafetensors(path, "00"); err == nil {
		t.Fatal("bad sha accepted")
	}
}

func TestRejectsOverlappingPlanes(t *testing.T) {
	path, sum := writeFixture(t, 1, 1, 0)
	if _, err := LoadKompressUltraSafetensors(path, sum); err == nil {
		t.Fatal("overlapping planes accepted")
	}
}
