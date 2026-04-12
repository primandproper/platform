package tableaccess

import (
	"testing"

	"github.com/shoenig/test"
)

func TestManager_CreateUser(T *testing.T) {
	T.Parallel()

	T.Run("returns ErrNotSupported", func(t *testing.T) {
		t.Parallel()

		m := NewManager()
		err := m.CreateUser(t.Context(), "user", "pass")
		test.ErrorIs(t, err, ErrNotSupported)
	})
}

func TestManager_DeleteUser(T *testing.T) {
	T.Parallel()

	T.Run("returns ErrNotSupported", func(t *testing.T) {
		t.Parallel()

		m := NewManager()
		err := m.DeleteUser(t.Context(), "user")
		test.ErrorIs(t, err, ErrNotSupported)
	})
}

func TestManager_CreateDatabase(T *testing.T) {
	T.Parallel()

	T.Run("returns ErrNotSupported", func(t *testing.T) {
		t.Parallel()

		m := NewManager()
		err := m.CreateDatabase(t.Context(), "db", "owner")
		test.ErrorIs(t, err, ErrNotSupported)
	})
}

func TestManager_DeleteDatabase(T *testing.T) {
	T.Parallel()

	T.Run("returns ErrNotSupported", func(t *testing.T) {
		t.Parallel()

		m := NewManager()
		err := m.DeleteDatabase(t.Context(), "db")
		test.ErrorIs(t, err, ErrNotSupported)
	})
}

func TestManager_UserExists(T *testing.T) {
	T.Parallel()

	T.Run("returns ErrNotSupported", func(t *testing.T) {
		t.Parallel()

		m := NewManager()
		exists, err := m.UserExists(t.Context(), "user")
		test.False(t, exists)
		test.ErrorIs(t, err, ErrNotSupported)
	})
}

func TestManager_DatabaseExists(T *testing.T) {
	T.Parallel()

	T.Run("returns ErrNotSupported", func(t *testing.T) {
		t.Parallel()

		m := NewManager()
		exists, err := m.DatabaseExists(t.Context(), "db")
		test.False(t, exists)
		test.ErrorIs(t, err, ErrNotSupported)
	})
}

func TestManager_GrantUserAccessToTable(T *testing.T) {
	T.Parallel()

	T.Run("returns ErrNotSupported", func(t *testing.T) {
		t.Parallel()

		m := NewManager()
		err := m.GrantUserAccessToTable(t.Context(), "user", "schema", "table", "SELECT")
		test.ErrorIs(t, err, ErrNotSupported)
	})
}

func TestManager_UserCanAccessDatabase(T *testing.T) {
	T.Parallel()

	T.Run("returns ErrNotSupported", func(t *testing.T) {
		t.Parallel()

		m := NewManager()
		canAccess, err := m.UserCanAccessDatabase(t.Context(), "user", "db")
		test.False(t, canAccess)
		test.ErrorIs(t, err, ErrNotSupported)
	})
}
