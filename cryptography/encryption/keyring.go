package encryption

import (
	"context"
	"fmt"

	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/observability/keys"
	"github.com/primandproper/platform-go/v10/observability/metrics"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const keyringName = "encryption_keyring"

// keyIDAttribute labels the rotation metrics. It is the whole reason key IDs
// are worth carrying: "how many reads are still landing on the key we retired"
// is a countable backlog rather than a hope, and it is the signal that says
// whether a rotation has finished.
const keyIDAttribute = "encryption.key.id"

// MaxKeyIDLength bounds a KeyID.
//
// The limit comes from the frame: a ciphertext stores its key ID length in one
// byte, because the alternative is a variable-length integer in a format that
// has to be parsed correctly by every future version of this package. 255 is
// far past any sane ID, and an ID approaching it is a sign that something
// other than an identifier is being stored.
const MaxKeyIDLength = 255

type (
	// RingKey is one key's identity paired with the Cipher that uses it.
	RingKey struct {
		// Cipher performs the actual encryption under this key.
		Cipher Cipher
		// ID names the key, and is written into every ciphertext the Cipher
		// produces through the ring.
		ID KeyID
	}

	// Keyring is an EncryptorDecryptor over several keys at once: it encrypts
	// with the current one and decrypts with whichever key a ciphertext names,
	// which is what makes rotation something other than a flag day.
	//
	// Rotation is deliberately lazy and the ring does not perform it. Naming a
	// new current key means new writes use it and old ciphertexts keep opening
	// under the keys they name; moving the old rows over is a re-encrypt on
	// next write plus a sweep for rows that are never written again. A ring
	// that re-encrypted eagerly would turn a configuration change into an
	// unbounded write amplification against the database.
	//
	// Retiring a key is therefore the dangerous operation, not adding one.
	// Drop a key from the ring while ciphertexts still name it and those rows
	// stop being readable — permanently, if the material is gone. The
	// decryption metrics exist to make that backlog visible before it becomes
	// that.
	Keyring struct {
		o11y            observability.Observer
		byID            map[KeyID]Cipher
		current         Cipher
		encryptCounter  metrics.Int64Counter
		decryptCounter  metrics.Int64Counter
		unknownKeyCount metrics.Int64Counter
		currentID       KeyID
	}
)

var _ EncryptorDecryptor = (*Keyring)(nil)

// NewKeyring builds a Keyring that encrypts under current and decrypts under
// any key in keys.
//
// current has to name one of keys. There is no default and no "first one
// wins": which key new data is written under is the single most consequential
// thing about this object, and inferring it from ordering would make a
// reordered config file silently change what encrypts production.
func NewKeyring(current KeyID, ringKeys []RingKey, opts ...Option) (*Keyring, error) {
	if len(ringKeys) == 0 {
		return nil, ErrEmptyKeyring
	}

	o := newOptions(opts)

	byID := make(map[KeyID]Cipher, len(ringKeys))

	for i := range ringKeys {
		k := &ringKeys[i]

		switch {
		case k.ID == "":
			return nil, ErrEmptyKeyID
		case len(k.ID) > MaxKeyIDLength:
			return nil, fmt.Errorf("%w: %d bytes exceeds %d", ErrKeyIDTooLong, len(k.ID), MaxKeyIDLength)
		case k.Cipher == nil:
			return nil, fmt.Errorf("%w: key %q has no cipher", ErrNilCipher, k.ID)
		}

		if _, seen := byID[k.ID]; seen {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateKeyID, k.ID)
		}

		byID[k.ID] = k.Cipher
	}

	currentCipher, ok := byID[current]
	if !ok {
		return nil, fmt.Errorf("%w: %q is not among the ring's keys", ErrNoCurrentKey, current)
	}

	mp := metrics.EnsureMetricsProvider(o.metricsProvider)

	encryptCounter, err := mp.NewInt64Counter(keyringName + "_encryptions")
	if err != nil {
		return nil, fmt.Errorf("creating encryption counter: %w", err)
	}

	decryptCounter, err := mp.NewInt64Counter(keyringName + "_decryptions")
	if err != nil {
		return nil, fmt.Errorf("creating decryption counter: %w", err)
	}

	unknownKeyCount, err := mp.NewInt64Counter(keyringName + "_unknown_key_ids")
	if err != nil {
		return nil, fmt.Errorf("creating unknown key counter: %w", err)
	}

	return &Keyring{
		o11y:            observability.NewObserver(keyringName, o.logger, o.tracerProvider),
		byID:            byID,
		current:         currentCipher,
		currentID:       current,
		encryptCounter:  encryptCounter,
		decryptCounter:  decryptCounter,
		unknownKeyCount: unknownKeyCount,
	}, nil
}

// CurrentKeyID reports the key new ciphertexts are written under. A sweep that
// re-encrypts stale rows needs it to know what "stale" means.
func (r *Keyring) CurrentKeyID() KeyID {
	return r.currentID
}

// KeyIDs reports every key the ring can decrypt with, current included, in no
// particular order.
func (r *Keyring) KeyIDs() []KeyID {
	ids := make([]KeyID, 0, len(r.byID))
	for id := range r.byID {
		ids = append(ids, id)
	}

	return ids
}

func (r *Keyring) Encrypt(ctx context.Context, plaintext, associatedData []byte) ([]byte, error) {
	ctx, op := r.o11y.Begin(ctx, observability.WithValue(keys.LengthKey, len(plaintext)))
	defer op.End()

	op.Set(keyIDAttribute, string(r.currentID))

	header, err := encodeHeader(r.currentID)
	if err != nil {
		return nil, op.Error(err, "encoding ciphertext header")
	}

	// The header is authenticated, not just prepended. Without this an
	// attacker could rewrite the key ID on a stored ciphertext and steer
	// decryption at a different key; the frame would still parse and the only
	// thing standing in the way would be that the wrong key happens to fail.
	sealed, err := r.current.Seal(ctx, plaintext, bindHeader(header, associatedData))
	if err != nil {
		return nil, op.Error(err, "sealing plaintext")
	}

	r.encryptCounter.Add(ctx, 1, metric.WithAttributes(attribute.String(keyIDAttribute, string(r.currentID))))

	return append(header, sealed...), nil
}

func (r *Keyring) Decrypt(ctx context.Context, ciphertext, associatedData []byte) ([]byte, error) {
	ctx, op := r.o11y.Begin(ctx, observability.WithValue(keys.LengthKey, len(ciphertext)))
	defer op.End()

	keyID, header, body, err := decodeHeader(ciphertext)
	if err != nil {
		return nil, op.Error(err, "decoding ciphertext header")
	}

	op.Set(keyIDAttribute, string(keyID))

	cipher, ok := r.byID[keyID]
	if !ok {
		r.unknownKeyCount.Add(ctx, 1, metric.WithAttributes(attribute.String(keyIDAttribute, string(keyID))))

		return nil, op.Error(fmt.Errorf("%w: %q", ErrUnknownKeyID, keyID), "resolving ciphertext key")
	}

	plaintext, err := cipher.Open(ctx, body, bindHeader(header, associatedData))
	if err != nil {
		return nil, op.Error(err, "opening ciphertext")
	}

	// Counted per key rather than in aggregate, so a rotation's tail is
	// visible: decryptions still attributed to a retired key are exactly the
	// rows a sweep has not reached yet.
	r.decryptCounter.Add(ctx, 1, metric.WithAttributes(attribute.String(keyIDAttribute, string(keyID))))

	return plaintext, nil
}
