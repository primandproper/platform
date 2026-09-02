package database

import (
	platformerrors "github.com/primandproper/platform-go/v14/errors"
)

var (
	// ErrUserAlreadyExists indicates that a user with that username has already been created.
	ErrUserAlreadyExists = platformerrors.New("user already exists")

	// ErrTransactionClosed indicates a Tx was used after the callback it was handed to
	// returned. The transaction has already been committed or rolled back, so the
	// statement was not run and never will be. It is returned rather than panicked
	// because the caller is usually a goroutine that outlived its callback, and a panic
	// there takes down a process for a write that was already lost.
	ErrTransactionClosed = platformerrors.New("transaction is closed")
)
