package imap

import "golang.org/x/crypto/bcrypt"

// checkPassword verifies a plaintext password against a bcrypt hash.
// Returns true if the password matches.
func checkPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}
