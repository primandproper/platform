package auditcfg

import (
	"context"

	"github.com/primandproper/platform-go/v10/audit"
	"github.com/primandproper/platform-go/v10/database"
	"github.com/primandproper/platform-go/v10/observability"

	"github.com/samber/do/v2"
)

// RegisterRecorder registers an audit.Recorder with the injector. The Recorder
// enlists in the caller's transaction and so needs no database.Client of its
// own.
//
// Prerequisites: *Config must be registered in the injector before the
// Recorder is invoked.
func RegisterRecorder(i do.Injector) {
	do.Provide[audit.Recorder](i, func(i do.Injector) (audit.Recorder, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		return NewRecorder(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			WithPillars(pillars),
		)
	})
}

// RegisterReader registers an audit.Reader with the injector.
//
// Prerequisites: *Config and database.Client must be registered in the
// injector before the Reader is invoked.
func RegisterReader(i do.Injector) {
	do.Provide[audit.Reader](i, func(i do.Injector) (audit.Reader, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		return NewReader(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			do.MustInvoke[database.Client](i),
			WithPillars(pillars),
		)
	})
}

// RegisterSweeper registers an *audit.Sweeper with the injector.
//
// Prerequisites: *Config and database.Client must be registered in the
// injector before the Sweeper is invoked.
func RegisterSweeper(i do.Injector) {
	do.Provide[*audit.Sweeper](i, func(i do.Injector) (*audit.Sweeper, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		return NewSweeper(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			do.MustInvoke[database.Client](i),
			WithPillars(pillars),
		)
	})
}
