package secrets

import (
	"bytes"
	"testing"
)

func TestSealRoundTripAndAADBinding(t *testing.T) {
	s, err := NewSealer(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := s.Seal([]byte("sk-secret"), []byte("purpose-a"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Open(sealed, []byte("purpose-a"))
	if err != nil || string(got) != "sk-secret" {
		t.Fatalf("Open = %q, %v", got, err)
	}
	if _, err := s.Open(sealed, []byte("purpose-b")); err == nil {
		t.Fatal("Open with different AAD succeeded")
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := s.Open(sealed, []byte("purpose-a")); err == nil {
		t.Fatal("Open of tampered value succeeded")
	}
}

func TestNewSealerRejectsShortKey(t *testing.T) {
	if _, err := NewSealer(make([]byte, 16)); err == nil {
		t.Fatal("expected error for 16-byte key")
	}
}

func TestHint(t *testing.T) {
	if got := Hint("sk-ant-0123456789abcd"); got != "…abcd" {
		t.Fatalf("Hint = %q", got)
	}
	if got := Hint("short"); got != "…" {
		t.Fatalf("Hint(short) = %q", got)
	}
}
