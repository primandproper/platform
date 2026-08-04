package service

import (
	"fmt"
)

// Option configures what New assembles.
//
// It exists for the half of a service this package cannot see. Everything the
// config names, New finds on the injector; everything the application owns —
// its own loops, and in time its own health checks — arrives here, because no
// config can name a type this module does not define.
type Option func(*options)

// options collects what the options set.
type options struct {
	runners []named[Runner]
}

// newOptions applies opts, ignoring nil entries.
func newOptions(opts []Option) *options {
	o := &options{}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}

	return o
}

// WithRunners joins application-owned background loops to the service's
// lifecycle. Nil entries are ignored.
//
// They start after everything the config named and close before it, which is
// the only order that can be right without knowing what they do: an
// application loop is built from the platform's clients and loops, so it is the
// one thing guaranteed to be downstream of all of them. A loop that writes
// outbox rows is finished before the relay drains; one that enqueues jobs is
// finished before the pool stops consuming.
//
// This is also how a generic loop joins. eventcapture.Recorder[E] satisfies
// Runner, but a type argument is not something a config can supply, so the
// application builds one and hands it over here.
//
// Failures are reported under each runner's type name, since a Runner arrives
// as a value with nothing else to call it by.
func WithRunners(runners ...Runner) Option {
	return func(o *options) {
		for _, runner := range runners {
			if runner == nil {
				continue
			}

			o.runners = append(o.runners, named[Runner]{name: fmt.Sprintf("%T", runner), v: runner})
		}
	}
}
