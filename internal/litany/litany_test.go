package litany

import (
	"bytes"
	"testing"

	"mince/internal/pq"
)

func TestRoundTrip(t *testing.T) {
	f, err := Seal(KindProposal, 0x0002, CapRetrieve|CapFelix, pq.New(3, 4), [32]byte{1, 2, 3}, []byte("hello wire"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != HeaderSize+len(f.Payload) {
		t.Fatalf("frame length %d", len(b))
	}
	got, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindProposal || got.Flags != 2 || got.CapMask != f.CapMask {
		t.Fatalf("header mismatch: %+v", got.Header)
	}
	if got.Budget.String() != "3/4" {
		t.Fatalf("budget %s", got.Budget)
	}
	if got.PrevHash != f.PrevHash || !bytes.Equal(got.Payload, f.Payload) {
		t.Fatal("payload/prev mismatch")
	}
	if got.PayloadCRC != f.PayloadCRC {
		t.Fatal("crc mismatch")
	}
}

func TestCRCRejectsFlippedBit(t *testing.T) {
	f, _ := Seal(KindTelemetry, 0, 0, pq.New(0, 1), [32]byte{}, []byte("salience"))
	b, _ := f.Encode()
	b[HeaderSize+1] ^= 0x01
	if _, err := Decode(b); err == nil {
		t.Fatal("flipped payload bit accepted")
	}
	b[HeaderSize+1] ^= 0x01
	b[HeaderSize] ^= 0x80
	if _, err := Decode(b); err == nil {
		t.Fatal("flipped payload bit accepted")
	}
}

func TestChain(t *testing.T) {
	var frames []*Frame
	var prev [32]byte
	for i := 0; i < 3; i++ {
		f, err := Seal(KindVerdict, uint16(i), CapRalph, pq.New(1, 20), prev, []byte{byte(i)})
		if err != nil {
			t.Fatal(err)
		}
		frames = append(frames, f)
		prev = f.Hash()
	}
	if err := VerifyChain(frames, [32]byte{}); err != nil {
		t.Fatal(err)
	}
	frames[1].PrevHash[0] ^= 1
	if err := VerifyChain(frames, [32]byte{}); err == nil {
		t.Fatal("broken chain accepted")
	}
}

func TestPOLA(t *testing.T) {
	if !Allowed(CapRetrieve|CapFelix, CapRetrieve) {
		t.Fatal("granted subset rejected")
	}
	if Allowed(CapRetrieve, CapFelix) {
		t.Fatal("ungranted capability accepted")
	}
	if Allowed(0, CapExec) {
		t.Fatal("zero grant accepted exec")
	}
}
