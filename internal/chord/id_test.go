package chord

import "testing"

func TestNormalizeURIEnforcesHTTPS(t *testing.T) {
	if _, err := NormalizeURI("http://node.example.com"); err == nil {
		t.Fatal("expected http URI to be rejected")
	}
	got, err := NormalizeURI("https://NODE.Example.com:443/")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://node.example.com" {
		t.Fatalf("unexpected normalized URI: %s", got)
	}
	got, err = NormalizeURI("https://NODE.Example.com:8443/")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://node.example.com:8443" {
		t.Fatalf("unexpected normalized URI with port: %s", got)
	}
}

func TestInRangeOpenClosed(t *testing.T) {
	a := "000000000000000000000000000000000000000a"
	b := "0000000000000000000000000000000000000014"
	if !InRangeOpenClosed("0000000000000000000000000000000000000014", a, b) {
		t.Fatal("expected upper bound to be included")
	}
	if InRangeOpenClosed(a, a, b) {
		t.Fatal("expected lower bound to be excluded")
	}
	wrapA := "fffffffffffffffffffffffffffffffffffffffb"
	wrapB := "0000000000000000000000000000000000000005"
	if !InRangeOpenClosed("0000000000000000000000000000000000000001", wrapA, wrapB) {
		t.Fatal("expected wrapped low value to be included")
	}
	if !InRangeOpenClosed("fffffffffffffffffffffffffffffffffffffffe", wrapA, wrapB) {
		t.Fatal("expected wrapped high value to be included")
	}
}

func TestFingerStart(t *testing.T) {
	start, err := FingerStart("0000000000000000000000000000000000000000", 0)
	if err != nil {
		t.Fatal(err)
	}
	if start != "0000000000000000000000000000000000000001" {
		t.Fatalf("unexpected finger start: %s", start)
	}
	start, err = FingerStart("ffffffffffffffffffffffffffffffffffffffff", 0)
	if err != nil {
		t.Fatal(err)
	}
	if start != "0000000000000000000000000000000000000000" {
		t.Fatalf("expected wraparound to zero, got %s", start)
	}
}
