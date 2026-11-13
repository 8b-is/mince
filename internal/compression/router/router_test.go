package router

import "testing"

func TestClassify(t *testing.T) {
	if got := Classify([]byte(`{"a":1,"b":[2,3]}`)); got != KindJSON {
		t.Fatalf("json: %s", got)
	}
	log := []byte("2026-01-02 03:04:05 INFO boot\n2026-01-02 03:04:06 ERROR fail\n2026-01-02 03:04:07 WARN retry\n")
	if got := Classify(log); got != KindLog {
		t.Fatalf("log: %s", got)
	}
	code := []byte("package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n")
	if got := Classify(code); got != KindCode {
		t.Fatalf("code: %s", got)
	}
	if got := Classify([]byte("just some prose about a walk in the park.")); got != KindText {
		t.Fatalf("text: %s", got)
	}
}

func TestApproxTokens(t *testing.T) {
	if got := ApproxTokens([]byte("12345678")); got != 2 {
		t.Fatalf("got %d", got)
	}
}
