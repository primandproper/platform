package tracing

import (
	"runtime"
	"strings"
)

const (
	// We need the frame at index 3, since we never want runtime.Callers or getFunctionCaller or StartSpan itself.
	runtimeFrameBuffer = 3
	counterBuffer      = 2
)

var (
	PackagePrefix = "github.com/primandproper/platform/"
)

// GetCallerName is largely (and respectfully) inspired by/copied from https://stackoverflow.com/a/35213181
func GetCallerName() string {
	// Set size to targetFrameIndex+2 to ensure we have room for one more caller than we need
	programCounters := make([]uintptr, runtimeFrameBuffer+counterBuffer)
	n := runtime.Callers(0, programCounters)
	frame := runtime.Frame{Function: "unknown"}

	if n > 0 {
		frames := runtime.CallersFrames(programCounters[:n])

		for more, frameIndex := true, 0; more && frameIndex <= runtimeFrameBuffer; frameIndex++ {
			if frameIndex == runtimeFrameBuffer {
				frame, more = frames.Next()
			} else {
				_, more = frames.Next()
			}
		}
	}

	return strings.TrimPrefix(frame.Function, PackagePrefix)
}
