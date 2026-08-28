package notifications

import (
	"testing"
	"time"

	"github.com/shoenig/test"
)

func TestParsePlatform(T *testing.T) {
	T.Parallel()

	T.Run("normalizes what a mobile client sends", func(t *testing.T) {
		t.Parallel()

		// The string reaching this was typed by a client. "iOS" and "ios" are one
		// platform, and a registry that stored both would hold two rows for one
		// handset and prune neither when the provider rejected the token.
		for _, spelling := range []string{"ios", "iOS", "IOS", " ios ", "\tiOS\n"} {
			p, ok := ParsePlatform(spelling)
			test.True(t, ok, test.Sprintf("%q", spelling))
			test.EqOp(t, PlatformIOS, p, test.Sprintf("%q", spelling))
		}

		p, ok := ParsePlatform("Android")
		test.True(t, ok)
		test.EqOp(t, PlatformAndroid, p)
	})

	T.Run("refuses a platform nothing routes to", func(t *testing.T) {
		t.Parallel()

		for _, spelling := range []string{"", "web", "blackberry", "i os"} {
			_, ok := ParsePlatform(spelling)
			test.False(t, ok, test.Sprintf("%q", spelling))
		}
	})
}

func TestPlatform_Valid(t *testing.T) {
	t.Parallel()

	test.True(t, PlatformIOS.Valid())
	test.True(t, PlatformAndroid.Valid())
	test.False(t, Platform("").Valid())
	test.False(t, Platform("IOS").Valid())
}

func TestPlatform_String(t *testing.T) {
	t.Parallel()

	// The senders spell it lowercase, and this is what a store binds and what a
	// TokenInvalidator is handed.
	test.EqOp(t, "ios", PlatformIOS.String())
	test.EqOp(t, "android", PlatformAndroid.String())
}

func TestNotification_Read(t *testing.T) {
	t.Parallel()

	at := time.Now().UTC()

	test.False(t, (*Notification)(nil).Read())
	test.False(t, (&Notification{}).Read())
	test.True(t, (&Notification{ReadAt: &at}).Read())
}
