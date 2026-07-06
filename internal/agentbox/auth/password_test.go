package auth

import "testing"

func TestPasswordHashAndVerify(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" || !VerifyPassword("correct horse battery staple", hash) {
		t.Fatalf("password did not verify: %q", hash)
	}
	if VerifyPassword("wrong", hash) {
		t.Fatal("wrong password verified")
	}
	if VerifyPassword("correct horse battery staple", "plaintext") {
		t.Fatal("unsupported hash verified")
	}
}
