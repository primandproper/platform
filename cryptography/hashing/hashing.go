package hashing

type (
	Hasher interface {
		Hash(content string) (string, error)
	}
)
