package errors

import (
	"errors"
	"fmt"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestSentinelErrors(T *testing.T) {
	T.Parallel()

	T.Run("ErrNilInputParameter", func(t *testing.T) {
		t.Parallel()
		test.NotNil(t, ErrNilInputParameter)
		test.StrContains(t, ErrNilInputParameter.Error(), "nil")
	})

	T.Run("ErrEmptyInputParameter", func(t *testing.T) {
		t.Parallel()
		test.NotNil(t, ErrEmptyInputParameter)
		test.StrContains(t, ErrEmptyInputParameter.Error(), "empty")
	})

	T.Run("ErrNilInputProvided", func(t *testing.T) {
		t.Parallel()
		test.NotNil(t, ErrNilInputProvided)
		test.StrContains(t, ErrNilInputProvided.Error(), "nil input")
	})

	T.Run("ErrInvalidIDProvided", func(t *testing.T) {
		t.Parallel()
		test.NotNil(t, ErrInvalidIDProvided)
		test.StrContains(t, ErrInvalidIDProvided.Error(), "ID")
	})

	T.Run("ErrEmptyInputProvided", func(t *testing.T) {
		t.Parallel()
		test.NotNil(t, ErrEmptyInputProvided)
		test.StrContains(t, ErrEmptyInputProvided.Error(), "empty")
	})

	T.Run("sentinels are distinct", func(t *testing.T) {
		t.Parallel()
		test.False(t, errors.Is(ErrNilInputParameter, ErrEmptyInputParameter))
		test.False(t, errors.Is(ErrNilInputProvided, ErrInvalidIDProvided))
		test.False(t, errors.Is(ErrEmptyInputProvided, ErrNilInputProvided))
	})
}

func TestNew(T *testing.T) {
	T.Parallel()

	T.Run("creates error with message", func(t *testing.T) {
		t.Parallel()
		err := New("test error")
		must.Error(t, err)
		test.EqError(t, err, "test error")
	})
}

func TestNewf(T *testing.T) {
	T.Parallel()

	T.Run("creates formatted error", func(t *testing.T) {
		t.Parallel()
		err := Newf("error %d: %s", 42, "details")
		must.Error(t, err)
		test.StrContains(t, err.Error(), "42")
		test.StrContains(t, err.Error(), "details")
	})
}

func TestErrorf(T *testing.T) {
	T.Parallel()

	T.Run("creates formatted error", func(t *testing.T) {
		t.Parallel()
		err := Errorf("something %s", "failed")
		must.Error(t, err)
		test.StrContains(t, err.Error(), "something failed")
	})
}

func TestWrap(T *testing.T) {
	T.Parallel()

	T.Run("wraps error with message", func(t *testing.T) {
		t.Parallel()
		inner := fmt.Errorf("inner")
		wrapped := Wrap(inner, "outer")
		must.Error(t, wrapped)
		test.ErrorIs(t, wrapped, inner)
		test.StrContains(t, wrapped.Error(), "outer")
	})

	T.Run("nil error returns nil", func(t *testing.T) {
		t.Parallel()
		test.Nil(t, Wrap(nil, "outer"))
	})
}

func TestWrapf(T *testing.T) {
	T.Parallel()

	T.Run("wraps error with formatted message", func(t *testing.T) {
		t.Parallel()
		inner := fmt.Errorf("inner")
		wrapped := Wrapf(inner, "outer %d", 1)
		must.Error(t, wrapped)
		test.ErrorIs(t, wrapped, inner)
		test.StrContains(t, wrapped.Error(), "outer 1")
	})
}
