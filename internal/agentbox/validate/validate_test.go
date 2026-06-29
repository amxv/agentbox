package validate

import "testing"

func TestValidators(t *testing.T) {
	if err := CreateThreadTitle(""); err == nil {
		t.Fatal("expected empty title error")
	}
	if err := CreateThreadTitle("ok"); err != nil {
		t.Fatal(err)
	}
	if err := FileReference("not-a-url", "file_1"); err == nil || err.Error() != "Invalid URL" {
		t.Fatalf("expected invalid URL error, got %v", err)
	}
	if got := ClampSignedURLExpiry(1); got != 60 {
		t.Fatalf("ClampSignedURLExpiry(1) = %d", got)
	}
	if got := ClampSignedURLExpiry(4000); got != 3600 {
		t.Fatalf("ClampSignedURLExpiry(4000) = %d", got)
	}
}
