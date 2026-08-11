package tableaccess

import (
	"context"

	"github.com/primandproper/platform-go/v10/database"
	"github.com/primandproper/platform-go/v10/errors"
)

// ErrNotSupported is returned for operations that SQLite does not support.
// SQLite has no concept of users, roles, permissions, or multiple databases.
var ErrNotSupported = errors.New("operation not supported by SQLite")

var _ database.Manager = (*Manager)(nil)

// Manager is the SQLite database.Manager implementation: every operation
// reports ErrNotSupported, because SQLite has no users, roles, or grants. It is
// exported, and returned by NewManager, so a caller who has chosen SQLite can
// depend on that choice rather than on the interface every dialect's manager
// shares.
type Manager struct{}

func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) CreateUser(_ context.Context, _, _ string) error {
	return ErrNotSupported
}

func (m *Manager) DeleteUser(_ context.Context, _ string) error {
	return ErrNotSupported
}

func (m *Manager) CreateDatabase(_ context.Context, _, _ string) error {
	return ErrNotSupported
}

func (m *Manager) DeleteDatabase(_ context.Context, _ string) error {
	return ErrNotSupported
}

func (m *Manager) UserExists(_ context.Context, _ string) (bool, error) {
	return false, ErrNotSupported
}

func (m *Manager) DatabaseExists(_ context.Context, _ string) (bool, error) {
	return false, ErrNotSupported
}

func (m *Manager) GrantUserAccessToTable(_ context.Context, _, _, _, _ string) error {
	return ErrNotSupported
}

func (m *Manager) UserCanAccessDatabase(_ context.Context, _, _ string) (bool, error) {
	return false, ErrNotSupported
}
