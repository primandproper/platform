package noop

import (
	"github.com/primandproper/platform-go/v10/distributedlock"

	"github.com/samber/do/v2"
)

// RegisterLocker registers this implementation under two keys: its own type,
// *Locker, and distributedlock.Locker. Both resolve to the same locker.
//
// Naming the noop is the whole point of registering it. A container that simply
// leaves the locker out gets a do.Invoke failure, which is the right answer for
// a dependency somebody forgot; a container that registers this one has said
// its deployment has nothing to coordinate with, and that every Acquire
// succeeding is the intended behavior rather than an accident.
//
// Nothing is invoked and nothing is configurable: a locker that coordinates
// nothing has no knobs and no observability to attach.
func RegisterLocker(i do.Injector) {
	do.Provide(i, func(do.Injector) (*Locker, error) {
		return NewLocker(), nil
	})

	// Cannot fail: *Locker implements distributedlock.Locker — the compiler
	// says so at the top of noop.go — and the service it aliases was just
	// provided.
	do.MustAs[*Locker, distributedlock.Locker](i)
}

// RegisterScopedLocker registers this implementation's scoped locker under two
// keys: its own type, *ScopedLocker, and distributedlock.ScopedLocker. Both
// resolve to the same locker, which runs every fn immediately.
func RegisterScopedLocker(i do.Injector) {
	do.Provide(i, func(do.Injector) (*ScopedLocker, error) {
		return NewScopedLocker(), nil
	})

	// Cannot fail: *ScopedLocker implements distributedlock.ScopedLocker — the
	// compiler says so in noop.go — and the service it aliases was just
	// provided.
	do.MustAs[*ScopedLocker, distributedlock.ScopedLocker](i)
}
