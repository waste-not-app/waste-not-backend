package security

import "github.com/alexedwards/argon2id"

var PasswordParams = &argon2id.Params{
	Memory:      19 * 1024,
	Iterations:  2,
	Parallelism: 1,
	SaltLength:  16,
	KeyLength:   32,
}

func Hash(password string) (string, error) {
	return argon2id.CreateHash(password, PasswordParams)
}

func Verify(password, hash string) bool {
	ok, err := argon2id.ComparePasswordAndHash(password, hash)
	return err == nil && ok
}
