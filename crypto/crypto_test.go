package crypto

import (
	"bytes"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	plain := []byte("# rbac-policy v1\nb, u, r, , 1, ALL\n")
	for _, name := range []string{NameAES256GCM, NameAES128GCM} {
		t.Run(name, func(t *testing.T) {
			alg, err := Lookup(name)
			if err != nil {
				t.Fatal(err)
			}
			key, err := alg.GenerateKey()
			if err != nil {
				t.Fatal(err)
			}
			wrapped, err := Seal(alg, key, plain)
			if err != nil {
				t.Fatal(err)
			}
			if !IsSealed(wrapped) || bytes.Contains(wrapped, []byte("b, u")) {
				t.Fatalf("envelope leaked plaintext: %s", wrapped)
			}
			got, err := Open(key, wrapped)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, plain) {
				t.Fatalf("got %s", got)
			}
			if _, err := Open(bytes.Repeat([]byte{1}, len(key)), wrapped); err == nil {
				t.Fatal("wrong key should fail")
			}
		})
	}
}

func TestLookupDefault(t *testing.T) {
	a, err := Lookup("")
	if err != nil || a.Name() != DefaultName {
		t.Fatalf("%v %v", a, err)
	}
	if _, err := Lookup("md5"); err == nil {
		t.Fatal("unknown alg")
	}
}
