package database

import (
	"testing"

	"github.com/shoenig/test"
)

func TestErrUserAlreadyExists(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		test.NotNil(t, ErrUserAlreadyExists)
		test.StrContains(t, ErrUserAlreadyExists.Error(), "user already exists")
	})
}

func TestErrDatabaseNotReady(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		test.NotNil(t, ErrDatabaseNotReady)
		test.StrContains(t, ErrDatabaseNotReady.Error(), "database is not ready yet")
	})
}
