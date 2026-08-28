package mobile

import (
	"context"
	"errors"
	"testing"

	platformerrors "github.com/primandproper/platform-go/v13/errors"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// errUnreachable stands in for a send that failed for a reason the token has
// nothing to do with.
var errUnreachable = platformerrors.New("the provider is unreachable")

// stubAPNs and stubFCM answer one send with whatever a test hands them, and
// record what they were asked to send.
type stubAPNs struct {
	err   error
	token string
	calls int
}

func (s *stubAPNs) Send(_ context.Context, deviceToken, _, _ string, _ *int) error {
	s.calls++
	s.token = deviceToken

	return s.err
}

type stubFCM struct {
	err   error
	token string
	calls int
}

func (s *stubFCM) Send(_ context.Context, deviceToken, _, _ string) error {
	s.calls++
	s.token = deviceToken

	return s.err
}

// recordingInvalidator is the registry half of the feedback loop, without a
// database.
type recordingInvalidator struct {
	err       error
	platforms []string
	tokens    []string
}

var _ TokenInvalidator = (*recordingInvalidator)(nil)

func (r *recordingInvalidator) InvalidateDeviceToken(_ context.Context, platform, token string) error {
	r.platforms = append(r.platforms, platform)
	r.tokens = append(r.tokens, token)

	return r.err
}

func TestMultiPlatformPushSender_TokenInvalidation(T *testing.T) {
	T.Parallel()

	ctx := T.Context()

	T.Run("a dead ios token is reported to the registry", func(t *testing.T) {
		t.Parallel()

		invalidator := &recordingInvalidator{}
		sender := NewMultiPlatformPushSender(
			&stubAPNs{err: platformerrors.Wrap(ErrTokenInvalid, "apns: Unregistered")},
			nil,
			WithTokenInvalidator(invalidator))

		err := sender.SendPush(ctx, "iOS", "dead-token", PushMessage{Title: "t", Body: "b"})

		// The send's error comes back either way: the caller asked whether the
		// push arrived, and it did not.
		test.ErrorIs(t, err, ErrTokenInvalid)

		must.SliceLen(t, 1, invalidator.tokens)
		test.EqOp(t, "dead-token", invalidator.tokens[0])

		// The platform reaches the registry normalized, because SendPush
		// normalized it before routing on it.
		test.EqOp(t, "ios", invalidator.platforms[0])
	})

	T.Run("a dead android token is reported to the registry", func(t *testing.T) {
		t.Parallel()

		invalidator := &recordingInvalidator{}
		sender := NewMultiPlatformPushSender(nil,
			&stubFCM{err: platformerrors.Wrap(ErrTokenInvalid, "fcm: UNREGISTERED")},
			WithTokenInvalidator(invalidator))

		err := sender.SendPush(ctx, "android", "dead-token", PushMessage{Title: "t", Body: "b"})
		test.ErrorIs(t, err, ErrTokenInvalid)

		must.SliceLen(t, 1, invalidator.tokens)
		test.EqOp(t, "android", invalidator.platforms[0])
		test.EqOp(t, "dead-token", invalidator.tokens[0])
	})

	T.Run("a transport failure prunes nothing", func(t *testing.T) {
		t.Parallel()

		// The distinction the whole seam exists for. A push that failed because
		// APNs was unreachable is a push to retry, and deleting the row would
		// unregister a handset that is working fine.
		invalidator := &recordingInvalidator{}
		sender := NewMultiPlatformPushSender(&stubAPNs{err: errUnreachable}, nil,
			WithTokenInvalidator(invalidator))

		err := sender.SendPush(ctx, "ios", "live-token", PushMessage{Title: "t", Body: "b"})
		test.ErrorIs(t, err, errUnreachable)
		test.SliceEmpty(t, invalidator.tokens)
	})

	T.Run("a successful send prunes nothing", func(t *testing.T) {
		t.Parallel()

		invalidator := &recordingInvalidator{}
		apns := &stubAPNs{}
		sender := NewMultiPlatformPushSender(apns, nil, WithTokenInvalidator(invalidator))

		must.NoError(t, sender.SendPush(ctx, "ios", "live-token", PushMessage{Title: "t", Body: "b"}))
		test.EqOp(t, 1, apns.calls)
		test.SliceEmpty(t, invalidator.tokens)
	})

	T.Run("without an invalidator the classification still reaches the caller", func(t *testing.T) {
		t.Parallel()

		// Unwired is the state every deployment was in before the hook existed:
		// nothing prunes, and nothing is hidden either.
		sender := NewMultiPlatformPushSender(
			&stubAPNs{err: platformerrors.Wrap(ErrTokenInvalid, "apns: BadDeviceToken")}, nil)

		err := sender.SendPush(ctx, "ios", "dead-token", PushMessage{Title: "t", Body: "b"})
		test.ErrorIs(t, err, ErrTokenInvalid)
	})

	T.Run("a failing invalidator does not replace the send's diagnosis", func(t *testing.T) {
		t.Parallel()

		// The push has already failed and the token is already known dead. The
		// worst answer available here is replacing that with "the database was
		// busy", which is what returning the prune's error would do.
		invalidator := &recordingInvalidator{err: errUnreachable}
		sender := NewMultiPlatformPushSender(
			&stubAPNs{err: platformerrors.Wrap(ErrTokenInvalid, "apns: Unregistered")}, nil,
			WithTokenInvalidator(invalidator))

		err := sender.SendPush(ctx, "ios", "dead-token", PushMessage{Title: "t", Body: "b"})
		test.ErrorIs(t, err, ErrTokenInvalid)
		test.False(t, errors.Is(err, errUnreachable))
		must.SliceLen(t, 1, invalidator.tokens)
	})

	T.Run("an unconfigured platform is not a dead token", func(t *testing.T) {
		t.Parallel()

		invalidator := &recordingInvalidator{}
		sender := NewMultiPlatformPushSender(nil, nil, WithTokenInvalidator(invalidator))

		test.ErrorIs(t,
			sender.SendPush(ctx, "ios", "token", PushMessage{Title: "t", Body: "b"}),
			ErrPlatformNotSupported)
		test.SliceEmpty(t, invalidator.tokens)
	})
}
