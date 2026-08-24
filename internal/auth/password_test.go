package auth

import "testing"

func TestPasswordHashVerify(t *testing.T) {
	hasher, err := NewPasswordHasher()
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	hash, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	matched, err := hasher.Verify("correct horse battery staple", hash)
	if err != nil || !matched {
		t.Fatalf("verify correct password: matched=%v err=%v", matched, err)
	}
	matched, err = hasher.Verify("incorrect password value", hash)
	if err != nil || matched {
		t.Fatalf("verify wrong password: matched=%v err=%v", matched, err)
	}
}

func TestPasswordValidationRejectsShortValue(t *testing.T) {
	if err := ValidatePassword("too-short"); err == nil {
		t.Fatal("short password accepted")
	}
}
