package cookies

import (
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/securecookie"
	"github.com/shoenig/test/must"
)

// A session cookie is encoded once per login and decoded on every authenticated
// request thereafter, so Decode is the row that matters and Encode is the one it
// is worth comparing against.
//
// securecookie does the work — HMAC, then AES — and the numbers below are
// mostly its. They are here so that the cost of carrying session state in a
// cookie is a measured quantity rather than an assumption, and so the o11y
// wrapper this package adds around it can be seen for what it is next to that.

// benchSessionValue is the shape a real session cookie carries, rather than a
// bare string: securecookie gob-encodes whatever it is given, so the value's
// complexity is part of the cost.
type benchSessionValue struct {
	CreatedAt time.Time
	UserID    string
	AccountID string
	Scopes    []string
}

func benchManager(b *testing.B) Manager {
	b.Helper()

	// Deterministic keys, so the benchmark measures the same work every run.
	// 32 bytes for the hash key and 32 for the block key selects AES-256.
	hashKey := make([]byte, 32)
	blockKey := make([]byte, 32)

	for i := range hashKey {
		hashKey[i] = byte(i)
		blockKey[i] = byte(255 - i)
	}

	m, err := NewCookieManager(&Config{
		Base64EncodedHashKey:  base64.StdEncoding.EncodeToString(hashKey),
		Base64EncodedBlockKey: base64.StdEncoding.EncodeToString(blockKey),
		Domain:                "example.com",
		SameSite:              SameSiteLax,
		Lifetime:              time.Hour,
		SecureOnly:            true,
	})
	must.NoError(b, err)

	return m
}

func benchValue() *benchSessionValue {
	return &benchSessionValue{
		UserID:    "user_01HZY3K4M5N6P7Q8R9S0T1U2V3",
		AccountID: "acct_01HZY3K4M5N6P7Q8R9S0T1U2V3",
		CreatedAt: time.Date(2026, time.August, 3, 12, 30, 45, 0, time.UTC),
		Scopes:    []string{"read", "write"},
	}
}

func BenchmarkManager(b *testing.B) {
	m := benchManager(b)
	ctx := b.Context()
	value := benchValue()

	encoded, err := m.Encode(ctx, "session", value)
	must.NoError(b, err)

	b.Run("Encode", func(b *testing.B) {
		for b.Loop() {
			stringSink, _ = m.Encode(ctx, "session", value)
		}
	})

	// The per-request row: every authenticated request decodes one of these.
	b.Run("Decode", func(b *testing.B) {
		for b.Loop() {
			var out benchSessionValue
			_ = m.Decode(ctx, "session", encoded, &out)
		}
	})

	// A tampered or expired cookie takes the refusal path, which has to stay
	// cheap: an attacker chooses how often it runs.
	b.Run("Decode/rejected", func(b *testing.B) {
		tampered := encoded[:len(encoded)-4] + "AAAA"

		for b.Loop() {
			var out benchSessionValue
			_ = m.Decode(ctx, "session", tampered, &out)
		}
	})

	// BuildCookie is Encode plus the http.Cookie the caller would otherwise
	// assemble themselves, so the delta is what the convenience costs.
	b.Run("BuildCookie", func(b *testing.B) {
		for b.Loop() {
			cookieSink, _ = m.BuildCookie(ctx, "session", value)
		}
	})
}

// BenchmarkSerializers prices the choice securecookie makes on this package's
// behalf.
//
// securecookie defaults to gob, which carries no per-instance stream to
// amortize type descriptors against and so re-emits them on every single call.
// That is the same finding the cache package's codec benchmarks record, and the
// reason gob is not the default there either — a cookie, like a cache entry, is
// encoded one value at a time, which is precisely the shape gob is worst at.
//
// The trade is not free and this benchmark does not settle it: the serializer
// is part of the cookie's wire format, so changing it invalidates every cookie
// already in the wild. That is a session-wide logout, to be scheduled rather
// than shipped quietly. What the rows below establish is what such a migration
// would actually buy, so the question can be decided on a number.
func BenchmarkSerializers(b *testing.B) {
	hashKey := make([]byte, 32)
	blockKey := make([]byte, 32)

	for i := range hashKey {
		hashKey[i] = byte(i)
		blockKey[i] = byte(255 - i)
	}

	value := benchValue()

	serializers := []struct {
		serializer securecookie.Serializer
		name       string
	}{
		{name: "gob(default)", serializer: securecookie.GobEncoder{}},
		{name: "json", serializer: securecookie.JSONEncoder{}},
	}

	for i := range serializers {
		s := &serializers[i]

		sc := securecookie.New(hashKey, blockKey).SetSerializer(s.serializer)

		encoded, err := sc.Encode("session", value)
		must.NoError(b, err)

		b.Run(s.name+"/Encode", func(b *testing.B) {
			for b.Loop() {
				stringSink, _ = sc.Encode("session", value)
			}
		})

		b.Run(s.name+"/Decode", func(b *testing.B) {
			for b.Loop() {
				var out benchSessionValue
				_ = sc.Decode("session", encoded, &out)
			}

			b.ReportMetric(float64(len(encoded)), "cookie_bytes")
		})
	}
}

var (
	stringSink string
	cookieSink *http.Cookie
)
