package hashing

import (
	"testing"
)

func TestHasherInterfaceExists(T *testing.T) {
	T.Parallel()

	T.Run("interface is satisfiable", func(t *testing.T) {
		t.Parallel()

		var _ Hasher = (*mockHasher)(nil)
	})
}

type mockHasher struct{}

func (m *mockHasher) Hash(_ string) (string, error) {
	return "", nil
}
