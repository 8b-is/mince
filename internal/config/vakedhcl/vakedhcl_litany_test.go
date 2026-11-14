package vakedhcl

import (
	"testing"

	"mince/internal/litany"
	"mince/internal/pq"
)

func TestLitanyChainAndPOLA(t *testing.T) {
	var frames []*litany.Frame
	var prev [32]byte
	for i := 0; i < 3; i++ {
		f, err := litany.Seal(litany.KindVerdict, uint16(i), litany.CapRetrieve|litany.CapTelemetry, pq.New(1, 20), prev, []byte{byte(i)})
		if err != nil {
			t.Fatal(err)
		}
		frames = append(frames, f)
		prev = f.Hash()
	}
	if err := litany.VerifyChain(frames, [32]byte{}); err != nil {
		t.Fatal(err)
	}
	frames[1].PrevHash[0] ^= 1
	if err := litany.VerifyChain(frames, [32]byte{}); err == nil {
		t.Fatal("broken chain accepted")
	}
	if !litany.Allowed(litany.CapRetrieve|litany.CapTelemetry, litany.CapRetrieve) {
		t.Fatal("granted subset rejected")
	}
	if litany.Allowed(litany.CapRetrieve, litany.CapExec) {
		t.Fatal("ungranted capability accepted")
	}
}
