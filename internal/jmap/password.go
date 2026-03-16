package jmap

import "golang.org/x/crypto/bcrypt"

// checkPassword verifies a plaintext password against a bcrypt hash.
func checkPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
