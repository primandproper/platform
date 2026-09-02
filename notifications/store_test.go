package notifications

import (
	"testing"
	"time"

	"github.com/primandproper/platform-go/v14/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/filtering"
	"github.com/primandproper/platform-go/v14/pointer"
	"github.com/primandproper/platform-go/v14/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// TestSQLStore_SQLite runs the behavioral suite against SQLite, which is the
// engine every developer has. TestSQLStore_RealServers runs the identical suite
// against Postgres and MySQL — see containers_test.go.
func TestSQLStore_SQLite(T *testing.T) {
	T.Parallel()

	runStoreSuite(T, newSQLiteEnv(T))
}

// runStoreSuite is everything this store promises, run against whichever
// database it is handed.
//
// It is one function rather than a file of top-level tests because the three
// engines have to be held to the same behavior: the conflict clause, the
// placeholder rendering and the archived predicates are spelled three ways, and
// a suite that ran only against SQLite would prove the one spelling SQLite
// accepts.
func runStoreSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("inbox", func(t *testing.T) {
		t.Parallel()

		runInboxSuite(t, env)
	})

	t.Run("registry", func(t *testing.T) {
		t.Parallel()

		runRegistrySuite(t, env)
	})
}

func runInboxSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("a created notification comes back with what the database assigned", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		n := newNotification(testPrincipal, "order.shipped", "Your order shipped")
		must.NoError(t, store.CreateNotification(t.Context(), n))

		// The id is minted here and the creation time is the database's, read
		// back rather than left as a zero time a caller would serialize as a
		// date in the year one.
		test.NotEqOp(t, "", n.ID)
		test.False(t, n.CreatedAt.IsZero())
		test.Nil(t, n.ReadAt)

		read, err := store.GetNotification(t.Context(), testScope, testPrincipal, n.ID)
		must.NoError(t, err)

		test.EqOp(t, n.ID, read.ID)
		test.EqOp(t, "order.shipped", read.Topic)
		test.EqOp(t, "Your order shipped", read.Title)
		test.EqOp(t, "the body", read.Body)
		test.EqOp(t, "/orders/1", read.Link)
		test.EqOp(t, testScope, read.Scope)
		test.False(t, read.Read())
	})

	t.Run("an id the caller supplied is the id that is stored", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		n := newNotification(testPrincipal, "invite.received", "You were invited")
		n.ID = "notif_supplied"
		must.NoError(t, store.CreateNotification(t.Context(), n))

		read, err := store.GetNotification(t.Context(), testScope, testPrincipal, "notif_supplied")
		must.NoError(t, err)
		test.EqOp(t, "notif_supplied", read.ID)
	})

	t.Run("another principal's notification reads as absent", func(t *testing.T) {
		t.Parallel()

		// The failure this exists for: a get keyed on the scope alone would hand
		// one member of an account another member's inbox row by id.
		store := env.newStore(t)

		n := newNotification(otherPrincipal, "order.shipped", "Their order shipped")
		must.NoError(t, store.CreateNotification(t.Context(), n))

		_, err := store.GetNotification(t.Context(), testScope, testPrincipal, n.ID)
		test.ErrorIs(t, err, ErrNotificationNotFound)
	})

	t.Run("another scope's notification reads as absent", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		n := newNotification(testPrincipal, "order.shipped", "Somebody else's tenant")
		n.Scope = otherScope
		must.NoError(t, store.CreateNotification(t.Context(), n))

		_, err := store.GetNotification(t.Context(), testScope, testPrincipal, n.ID)
		test.ErrorIs(t, err, ErrNotificationNotFound)
	})

	t.Run("the inbox lists only this principal's notifications, in both directions", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		mine := make([]string, 0, 3)
		for _, title := range []string{"first", "second", "third"} {
			n := newNotification(testPrincipal, "order.shipped", title)
			must.NoError(t, store.CreateNotification(t.Context(), n))
			mine = append(mine, n.ID)
		}

		theirs := newNotification(otherPrincipal, "order.shipped", "not yours")
		must.NoError(t, store.CreateNotification(t.Context(), theirs))

		ascending, err := store.ListNotifications(t.Context(), testScope, testPrincipal, nil)
		must.NoError(t, err)
		must.SliceLen(t, 3, ascending.Data)
		test.EqOp(t, uint64(3), ascending.FilteredCount)

		for i, id := range mine {
			test.EqOp(t, id, ascending.Data[i].ID)
		}

		descending, err := store.ListNotifications(t.Context(), testScope, testPrincipal,
			&filtering.QueryFilter{SortBy: filtering.SortDescending})
		must.NoError(t, err)
		must.SliceLen(t, 3, descending.Data)

		// The same page walked the other way, which is a second statement rather
		// than a bound argument — so this is what proves the store picked it.
		for i, id := range mine {
			test.EqOp(t, id, descending.Data[len(mine)-1-i].ID)
		}
	})

	t.Run("the unread list carries the badge count", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		ids := make([]string, 0, 3)
		for _, title := range []string{"first", "second", "third"} {
			n := newNotification(testPrincipal, "order.shipped", title)
			must.NoError(t, store.CreateNotification(t.Context(), n))
			ids = append(ids, n.ID)
		}

		must.NoError(t, store.MarkNotificationRead(t.Context(), testScope, testPrincipal, ids[0]))

		// One page of one, and the count is still of everything unread rather
		// than of what came back — which is the whole reason this schema needs no
		// COUNT statement.
		unread, err := store.ListUnreadNotifications(t.Context(), testScope, testPrincipal,
			&filtering.QueryFilter{MaxResponseSize: pointer.To(uint16(1))})
		must.NoError(t, err)

		must.SliceLen(t, 1, unread.Data)
		test.EqOp(t, ids[1], unread.Data[0].ID)
		test.EqOp(t, uint64(2), unread.FilteredCount)
	})

	t.Run("marking read stamps once and stays stamped", func(t *testing.T) {
		t.Parallel()

		c := newStubClock()
		store := env.newStore(t, WithClock(c))

		n := newNotification(testPrincipal, "order.shipped", "Your order shipped")
		must.NoError(t, store.CreateNotification(t.Context(), n))

		must.NoError(t, store.MarkNotificationRead(t.Context(), testScope, testPrincipal, n.ID))

		read, err := store.GetNotification(t.Context(), testScope, testPrincipal, n.ID)
		must.NoError(t, err)
		must.NotNil(t, read.ReadAt)
		test.EqOp(t, baseTime, read.ReadAt.UTC())

		// A second mark is a success that changes nothing. Moving the stamp is
		// what turns "read last Tuesday" into "read on every list refresh", and
		// it is the reason the statement guards on the column being absent.
		c.advance(time.Hour)
		must.NoError(t, store.MarkNotificationRead(t.Context(), testScope, testPrincipal, n.ID))

		reread, err := store.GetNotification(t.Context(), testScope, testPrincipal, n.ID)
		must.NoError(t, err)
		must.NotNil(t, reread.ReadAt)
		test.EqOp(t, baseTime, reread.ReadAt.UTC())
	})

	t.Run("marking a notification that is not in the inbox reports it", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		// Zero rows means two things — already read, or not there — and this is
		// the half that must not be reported as success.
		err := store.MarkNotificationRead(t.Context(), testScope, testPrincipal, "notif_nonexistent")
		test.ErrorIs(t, err, ErrNotificationNotFound)

		theirs := newNotification(otherPrincipal, "order.shipped", "not yours")
		must.NoError(t, store.CreateNotification(t.Context(), theirs))

		err = store.MarkNotificationRead(t.Context(), testScope, testPrincipal, theirs.ID)
		test.ErrorIs(t, err, ErrNotificationNotFound)
	})

	t.Run("marking everything read counts what was unread", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		for _, title := range []string{"first", "second", "third"} {
			n := newNotification(testPrincipal, "order.shipped", title)
			must.NoError(t, store.CreateNotification(t.Context(), n))
		}

		theirs := newNotification(otherPrincipal, "order.shipped", "not yours")
		must.NoError(t, store.CreateNotification(t.Context(), theirs))

		count, err := store.MarkAllNotificationsRead(t.Context(), testScope, testPrincipal)
		must.NoError(t, err)
		test.EqOp(t, int64(3), count)

		// Idempotent, and the count says so.
		count, err = store.MarkAllNotificationsRead(t.Context(), testScope, testPrincipal)
		must.NoError(t, err)
		test.EqOp(t, int64(0), count)

		// The other principal's notification is untouched, which is what the
		// principal predicate on a statement with no id is there for.
		other, err := store.GetNotification(t.Context(), testScope, otherPrincipal, theirs.ID)
		must.NoError(t, err)
		test.False(t, other.Read())
	})

	t.Run("archiving takes it out of the inbox and keeps the row", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		n := newNotification(testPrincipal, "order.shipped", "Your order shipped")
		must.NoError(t, store.CreateNotification(t.Context(), n))

		must.NoError(t, store.ArchiveNotification(t.Context(), testScope, testPrincipal, n.ID))

		_, err := store.GetNotification(t.Context(), testScope, testPrincipal, n.ID)
		test.ErrorIs(t, err, ErrNotificationNotFound)

		live, err := store.ListNotifications(t.Context(), testScope, testPrincipal, nil)
		must.NoError(t, err)
		test.SliceEmpty(t, live.Data)

		// The row is still there for whoever asks later what somebody was told.
		archived, err := store.ListNotifications(t.Context(), testScope, testPrincipal,
			&filtering.QueryFilter{IncludeArchived: pointer.To(true)})
		must.NoError(t, err)
		must.SliceLen(t, 1, archived.Data)
		test.NotNil(t, archived.Data[0].ArchivedAt)

		// Archiving it again finds nothing, because an archived notification is
		// not in the inbox and this addresses the inbox.
		test.ErrorIs(t,
			store.ArchiveNotification(t.Context(), testScope, testPrincipal, n.ID),
			ErrNotificationNotFound)
	})

	t.Run("refuses a notification nobody could read", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		test.ErrorIs(t, store.CreateNotification(t.Context(), nil), ErrNilNotification)

		unscoped := newNotification(testPrincipal, "order.shipped", "Your order shipped")
		unscoped.Scope = tenancy.Scope{}
		test.ErrorIs(t, store.CreateNotification(t.Context(), unscoped), tenancy.ErrNoScope)

		unaddressed := newNotification("", "order.shipped", "Your order shipped")
		test.ErrorIs(t, store.CreateNotification(t.Context(), unaddressed), ErrEmptyPrincipal)

		untopiced := newNotification(testPrincipal, "", "Your order shipped")
		test.ErrorIs(t, store.CreateNotification(t.Context(), untopiced), ErrEmptyTopic)

		untitled := newNotification(testPrincipal, "order.shipped", "")
		test.Error(t, store.CreateNotification(t.Context(), untitled))
	})

	t.Run("refuses a read that names no scope or no principal", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		_, err := store.GetNotification(t.Context(), tenancy.Scope{}, testPrincipal, "notif_1")
		test.ErrorIs(t, err, tenancy.ErrNoScope)

		_, err = store.ListNotifications(t.Context(), tenancy.Scope{}, testPrincipal, nil)
		test.ErrorIs(t, err, tenancy.ErrNoScope)

		_, err = store.ListUnreadNotifications(t.Context(), testScope, "", nil)
		test.ErrorIs(t, err, ErrEmptyPrincipal)

		_, err = store.MarkAllNotificationsRead(t.Context(), testScope, "")
		test.ErrorIs(t, err, ErrEmptyPrincipal)

		test.ErrorIs(t,
			store.MarkNotificationRead(t.Context(), testScope, "", "notif_1"), ErrEmptyPrincipal)
		test.ErrorIs(t,
			store.ArchiveNotification(t.Context(), testScope, "", "notif_1"), ErrEmptyPrincipal)
	})
}

func runRegistrySuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("a registered device comes back with what the database assigned", func(t *testing.T) {
		t.Parallel()

		c := newStubClock()
		store := env.newStore(t, WithClock(c))

		d := newDevice(testPrincipal, PlatformIOS, "token-a")
		must.NoError(t, store.RegisterDevice(t.Context(), d))

		test.NotEqOp(t, "", d.ID)
		test.False(t, d.CreatedAt.IsZero())
		test.EqOp(t, baseTime, d.LastSeenAt.UTC())
		test.EqOp(t, PlatformIOS, d.Platform)
	})

	t.Run("re-registering the same handset keeps its identity", func(t *testing.T) {
		t.Parallel()

		c := newStubClock()
		store := env.newStore(t, WithClock(c))

		first := newDevice(testPrincipal, PlatformIOS, "token-a")
		must.NoError(t, store.RegisterDevice(t.Context(), first))

		c.advance(time.Hour)

		// A fresh value, as a client that has forgotten its registration id would
		// send: same token, new id. The row it converges on keeps the id the
		// first registration minted, and the caller is told so — otherwise they
		// would hold an id no row has and revoke nothing on sign-out.
		again := newDevice(testPrincipal, PlatformIOS, "token-a")
		must.NoError(t, store.RegisterDevice(t.Context(), again))

		test.EqOp(t, first.ID, again.ID)
		test.EqOp(t, first.CreatedAt, again.CreatedAt)
		test.EqOp(t, baseTime.Add(time.Hour), again.LastSeenAt.UTC())

		devices, err := store.ListDevices(t.Context(), testScope, testPrincipal, nil)
		must.NoError(t, err)
		test.SliceLen(t, 1, devices.Data)
	})

	t.Run("a handset that changes hands moves rather than fanning out", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		first := newDevice(testPrincipal, PlatformIOS, "token-a")
		must.NoError(t, store.RegisterDevice(t.Context(), first))

		// Somebody else signs in on the same phone. Two rows here would deliver
		// the previous owner's notifications to the new one.
		second := newDevice(otherPrincipal, PlatformIOS, "token-a")
		must.NoError(t, store.RegisterDevice(t.Context(), second))

		gone, err := store.ListDevices(t.Context(), testScope, testPrincipal, nil)
		must.NoError(t, err)
		test.SliceEmpty(t, gone.Data)

		moved, err := store.ListDevices(t.Context(), testScope, otherPrincipal, nil)
		must.NoError(t, err)
		must.SliceLen(t, 1, moved.Data)
		test.EqOp(t, first.ID, moved.Data[0].ID)
	})

	t.Run("the same token on two platforms is two devices", func(t *testing.T) {
		t.Parallel()

		// The two providers mint their tokens independently, so the platform is
		// half the key rather than a label on it.
		store := env.newStore(t)

		must.NoError(t, store.RegisterDevice(t.Context(), newDevice(testPrincipal, PlatformIOS, "token-a")))
		must.NoError(t, store.RegisterDevice(t.Context(), newDevice(testPrincipal, PlatformAndroid, "token-a")))

		devices, err := store.ListDevices(t.Context(), testScope, testPrincipal, nil)
		must.NoError(t, err)
		test.SliceLen(t, 2, devices.Data)
	})

	t.Run("the batched read answers a fan-out in one query", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		must.NoError(t, store.RegisterDevice(t.Context(), newDevice(testPrincipal, PlatformIOS, "token-a")))
		must.NoError(t, store.RegisterDevice(t.Context(), newDevice(testPrincipal, PlatformAndroid, "token-b")))
		must.NoError(t, store.RegisterDevice(t.Context(), newDevice(otherPrincipal, PlatformIOS, "token-c")))

		elsewhere := newDevice(testPrincipal, PlatformIOS, "token-d")
		elsewhere.Scope = otherScope
		must.NoError(t, store.RegisterDevice(t.Context(), elsewhere))

		devices, err := store.ListDevicesByPrincipals(t.Context(), testScope,
			[]string{testPrincipal, otherPrincipal})
		must.NoError(t, err)
		test.SliceLen(t, 3, devices)

		// An empty batch is an empty answer and no query — the statement has no
		// rendering of an empty set.
		none, err := store.ListDevicesByPrincipals(t.Context(), testScope, nil)
		must.NoError(t, err)
		test.SliceEmpty(t, none)
	})

	t.Run("revoking removes the row and reports a registration that was not there", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		d := newDevice(testPrincipal, PlatformIOS, "token-a")
		must.NoError(t, store.RegisterDevice(t.Context(), d))

		must.NoError(t, store.RevokeDevice(t.Context(), testScope, testPrincipal, d.ID))

		devices, err := store.ListDevices(t.Context(), testScope, testPrincipal, nil)
		must.NoError(t, err)
		test.SliceEmpty(t, devices.Data)

		test.ErrorIs(t,
			store.RevokeDevice(t.Context(), testScope, testPrincipal, d.ID), ErrDeviceNotFound)
	})

	t.Run("another principal cannot revoke this one's device", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		d := newDevice(testPrincipal, PlatformIOS, "token-a")
		must.NoError(t, store.RegisterDevice(t.Context(), d))

		test.ErrorIs(t,
			store.RevokeDevice(t.Context(), testScope, otherPrincipal, d.ID), ErrDeviceNotFound)
		test.ErrorIs(t,
			store.RevokeDevice(t.Context(), otherScope, testPrincipal, d.ID), ErrDeviceNotFound)
	})

	t.Run("the provider hook prunes across every scope and is idempotent", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		// Registered in a scope the hook is never told about, because a provider
		// answering a push names a token and nothing else.
		elsewhere := newDevice(testPrincipal, PlatformIOS, "token-dead")
		elsewhere.Scope = otherScope
		must.NoError(t, store.RegisterDevice(t.Context(), elsewhere))

		must.NoError(t, store.InvalidateDeviceToken(t.Context(), "ios", "token-dead"))

		devices, err := store.ListDevices(t.Context(), otherScope, testPrincipal, nil)
		must.NoError(t, err)
		test.SliceEmpty(t, devices.Data)

		// A token already gone is the state the caller asked for. Two workers
		// pruning the same dead token must not turn a push into a second failure.
		must.NoError(t, store.InvalidateDeviceToken(t.Context(), "ios", "token-dead"))
	})

	t.Run("the provider hook normalizes the platform it is handed", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		must.NoError(t, store.RegisterDevice(t.Context(), newDevice(testPrincipal, PlatformIOS, "token-a")))

		// A mobile client's spelling, which is what reaches a sender.
		must.NoError(t, store.InvalidateDeviceToken(t.Context(), " iOS ", "token-a"))

		devices, err := store.ListDevices(t.Context(), testScope, testPrincipal, nil)
		must.NoError(t, err)
		test.SliceEmpty(t, devices.Data)
	})

	t.Run("the provider hook refuses what it cannot act on", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		// A platform nothing routes to would delete nothing and report success,
		// which is exactly the silence this hook exists to end.
		test.ErrorIs(t,
			store.InvalidateDeviceToken(t.Context(), "blackberry", "token-a"), ErrUnknownPlatform)
		test.ErrorIs(t,
			store.InvalidateDeviceToken(t.Context(), "ios", ""), ErrEmptyToken)
	})

	t.Run("refuses a registration nothing could ever push to", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		test.ErrorIs(t, store.RegisterDevice(t.Context(), nil), ErrNilDevice)

		unscoped := newDevice(testPrincipal, PlatformIOS, "token-a")
		unscoped.Scope = tenancy.Scope{}
		test.ErrorIs(t, store.RegisterDevice(t.Context(), unscoped), tenancy.ErrNoScope)

		unaddressed := newDevice("", PlatformIOS, "token-a")
		test.ErrorIs(t, store.RegisterDevice(t.Context(), unaddressed), ErrEmptyPrincipal)

		unroutable := newDevice(testPrincipal, Platform("blackberry"), "token-a")
		test.ErrorIs(t, store.RegisterDevice(t.Context(), unroutable), ErrUnknownPlatform)

		tokenless := newDevice(testPrincipal, PlatformIOS, "")
		test.ErrorIs(t, store.RegisterDevice(t.Context(), tokenless), ErrEmptyToken)
	})

	t.Run("refuses a read that names no scope or no principal", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		_, err := store.ListDevices(t.Context(), tenancy.Scope{}, testPrincipal, nil)
		test.ErrorIs(t, err, tenancy.ErrNoScope)

		_, err = store.ListDevices(t.Context(), testScope, "", nil)
		test.ErrorIs(t, err, ErrEmptyPrincipal)

		_, err = store.ListDevicesByPrincipals(t.Context(), tenancy.Scope{}, []string{testPrincipal})
		test.ErrorIs(t, err, tenancy.ErrNoScope)

		test.ErrorIs(t,
			store.RevokeDevice(t.Context(), testScope, "", "device_1"), ErrEmptyPrincipal)
	})
}

func TestNewSQLStore(T *testing.T) {
	T.Parallel()

	T.Run("refuses a nil client", func(t *testing.T) {
		t.Parallel()

		_, err := NewSQLStore(nil)
		test.ErrorIs(t, err, ErrNilDatabaseClient)
		test.ErrorIs(t, err, platformerrors.ErrNilInputParameter)
	})

	T.Run("refuses a prefix that would not render an identifier", func(t *testing.T) {
		t.Parallel()

		env := newSQLiteEnv(t)

		_, err := NewSQLStore(env.client, WithTablePrefix("no-hyphens-allowed"))
		test.Error(t, err)
	})

	T.Run("reports the prefix it was built with", func(t *testing.T) {
		t.Parallel()

		env := newSQLiteEnv(t)

		store, err := NewSQLStore(env.client, WithTablePrefix("ntf"))
		must.NoError(t, err)
		test.EqOp(t, "ntf", store.TablePrefix())

		unprefixed, err := NewSQLStore(env.client)
		must.NoError(t, err)
		test.EqOp(t, DefaultTablePrefix, unprefixed.TablePrefix())
	})

	T.Run("ignores a nil option and a nil clock", func(t *testing.T) {
		t.Parallel()

		env := newSQLiteEnv(t)

		store, err := NewSQLStore(env.client, nil, WithClock(nil))
		must.NoError(t, err)
		must.NotNil(t, store)
		test.False(t, store.now().IsZero())
	})
}

func TestNotificationsdbDialect(T *testing.T) {
	T.Parallel()

	T.Run("maps every dialect this module supports", func(t *testing.T) {
		t.Parallel()

		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite} {
			mapped, err := notificationsdbDialect(d)
			must.NoError(t, err, must.Sprintf("dialect %s", d))
			test.NotEqOp(t, "", string(mapped))
		}
	})

	T.Run("names a dialect the querier was not generated for", func(t *testing.T) {
		t.Parallel()

		_, err := notificationsdbDialect(dialect.Dialect("cassandra"))
		test.ErrorIs(t, err, dialect.ErrUnsupported)
	})
}
