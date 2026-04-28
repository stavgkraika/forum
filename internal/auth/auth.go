// Package auth provides password hashing and validation utilities.
package auth

import (
	"net/mail"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// HashPassword hashes a plain-text password using bcrypt.
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

// CheckPassword reports whether password matches the stored bcrypt hash.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// ValidEmail reports whether the given string is a valid email address.
func ValidEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil && strings.Contains(email, "@")
}

// ValidPassword reports whether the password meets the minimum length requirement.
func ValidPassword(password string) bool { return len(password) >= 8 }

// ValidUsername reports whether the username is between 3 and 30 non-whitespace characters.
func ValidUsername(username string) bool {
	u := strings.TrimSpace(username)
	return len(u) >= 3 && len(u) <= 30
}
