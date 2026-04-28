package auth

import "testing"

func TestHashPassword(t *testing.T) {
	hash, err := HashPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "password123" {
		t.Fatal("password stored in plain text")
	}
}

func TestCheckPassword(t *testing.T) {
	hash, _ := HashPassword("password123")
	if !CheckPassword(hash, "password123") {
		t.Fatal("correct password should match")
	}
	if CheckPassword(hash, "wrongpassword") {
		t.Fatal("wrong password should not match")
	}
	if CheckPassword("notahash", "password123") {
		t.Fatal("invalid hash should not match")
	}
}

func TestValidEmail(t *testing.T) {
	cases := []struct {
		email string
		want  bool
	}{
		{"user@example.com", true},
		{"user+tag@example.co.uk", true},
		{"bad", false},
		{"@nodomain", false},
		{"noatsign", false},
		{"", false},
	}
	for _, c := range cases {
		if got := ValidEmail(c.email); got != c.want {
			t.Errorf("ValidEmail(%q) = %v, want %v", c.email, got, c.want)
		}
	}
}

func TestValidPassword(t *testing.T) {
	cases := []struct {
		password string
		want     bool
	}{
		{"password1", true},
		{"exactly8", true},
		{"short", false},
		{"", false},
		{"1234567", false},
		{"12345678", true},
	}
	for _, c := range cases {
		if got := ValidPassword(c.password); got != c.want {
			t.Errorf("ValidPassword(%q) = %v, want %v", c.password, got, c.want)
		}
	}
}

func TestValidUsername(t *testing.T) {
	cases := []struct {
		username string
		want     bool
	}{
		{"abc", true},
		{"validuser", true},
		{"a_very_long_username_that_is_30ch", false},
		{"ab", false},
		{"", false},
		{"   ", false},
		{"  ab ", false},
	}
	for _, c := range cases {
		if got := ValidUsername(c.username); got != c.want {
			t.Errorf("ValidUsername(%q) = %v, want %v", c.username, got, c.want)
		}
	}
}
