package encoding

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"encoding/xml"
	"io"

	"github.com/primandproper/platform-go/v8/errors"
	"github.com/primandproper/platform-go/v8/observability"
	"github.com/primandproper/platform-go/v8/observability/keys"

	"github.com/BurntSushi/toml"
	"github.com/keith-turner/ecoji/v2"
	"gopkg.in/yaml.v3"
)

type (
	// Marshaler renders a value as bytes in one content type. It is the
	// smallest thing most callers need, and it carries no transport: anything
	// that has to turn a value into bytes — a queue payload, a cache entry, a
	// database column — should depend on this rather than on ClientEncoder.
	Marshaler interface {
		Marshal(ctx context.Context, v any) ([]byte, error)
	}

	// Unmarshaler parses bytes of one content type into v.
	Unmarshaler interface {
		Unmarshal(ctx context.Context, data []byte, v any) error
	}

	// Codec is the transport-free pair, plus the content type it speaks.
	// Prefer it over ClientEncoder wherever io.Writer and io.Reader are not
	// part of the job.
	Codec interface {
		Marshaler
		Unmarshaler

		ContentType() string
	}

	// ClientEncoder is a Codec that can also stream. The streaming halves are
	// separated out because they are the parts tied to a transport; a caller
	// that only needs bytes should ask for Marshaler or Codec instead.
	ClientEncoder interface {
		Codec

		Encode(ctx context.Context, dest io.Writer, v any) error
		EncodeReader(ctx context.Context, data any) (io.Reader, error)
	}

	// clientEncoder is our concrete implementation of ClientEncoder.
	clientEncoder struct {
		o11y        observability.Observer
		contentType *contentType
	}
)

// marshalFuncFor returns the byte-oriented marshaler for a content type, and
// is the single dispatch every encode path in this package routes through.
//
// Byte-oriented deliberately. The streaming encoders — json.NewEncoder and its
// counterparts — append a trailing newline, so a package that mixed the two
// would answer the same question with two different byte slices depending on
// which entry point you happened to call. Routing everything through here is
// what makes MustEncodeJSON(v) equal json.Marshal(v) exactly, which callers
// that compare or store the bytes depend on.
func marshalFuncFor(ct ContentType) func(v any) ([]byte, error) {
	switch ct {
	case ContentTypeXML:
		return xml.Marshal
	case ContentTypeTOML:
		return tomlMarshalFunc
	case ContentTypeYAML:
		return yaml.Marshal
	case ContentTypeEmoji:
		return marshalEmoji
	default:
		return json.Marshal
	}
}

func (e *clientEncoder) Unmarshal(ctx context.Context, data []byte, v any) error {
	_, op := e.o11y.Begin(ctx)
	defer op.End()

	op.Set("data_length", len(data))
	var unmarshalFunc func(data []byte, v any) error

	switch e.contentType {
	case ContentTypeXML:
		unmarshalFunc = xml.Unmarshal
	case ContentTypeEmoji:
		unmarshalFunc = unmarshalEmoji
	case ContentTypeTOML:
		unmarshalFunc = toml.Unmarshal
	case ContentTypeYAML:
		unmarshalFunc = yaml.Unmarshal
	default:
		unmarshalFunc = json.Unmarshal
	}

	if err := unmarshalFunc(data, v); err != nil {
		return observability.PrepareError(err, op.Span(), "unmarshaling content")
	}

	op.Logger().Debug("unmarshalled")

	return nil
}

// Marshal renders v as bytes in this encoder's content type.
func (e *clientEncoder) Marshal(ctx context.Context, v any) ([]byte, error) {
	_, op := e.o11y.Begin(ctx)
	defer op.End()

	out, err := marshalFuncFor(e.contentType)(v)
	if err != nil {
		return nil, observability.PrepareError(err, op.Span(), "marshaling content")
	}

	op.Set(keys.LengthKey, len(out))

	return out, nil
}

func (e *clientEncoder) Encode(ctx context.Context, dest io.Writer, data any) error {
	ctx, op := e.o11y.Begin(ctx)
	defer op.End()

	out, err := e.Marshal(ctx, data)
	if err != nil {
		return observability.PrepareError(err, op.Span(), "encoding content")
	}

	if _, err = dest.Write(out); err != nil {
		return observability.PrepareError(err, op.Span(), "writing encoded content")
	}

	return nil
}

func marshalEmoji(v any) ([]byte, error) {
	var gobWriter bytes.Buffer
	if err := gob.NewEncoder(&gobWriter).Encode(v); err != nil {
		return nil, errors.Wrap(err, "encoding to gob")
	}

	r := bytes.NewBuffer(gobWriter.Bytes())
	w := bytes.NewBuffer([]byte{})

	// lord help me, I don't know why it's 76 here
	if err := ecoji.EncodeV2(r, w, 76); err != nil {
		return nil, errors.Wrap(err, "encoding to emoji")
	}

	return w.Bytes(), nil
}

func unmarshalEmoji(data []byte, v any) error {
	w := bytes.NewBuffer([]byte{})

	if err := ecoji.Decode(bytes.NewReader(data), w); err != nil {
		return errors.Wrap(err, "decoding emoji")
	}

	return gob.NewDecoder(w).Decode(v)
}

func tomlMarshalFunc(v any) ([]byte, error) {
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(v); err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

func (e *clientEncoder) EncodeReader(ctx context.Context, data any) (io.Reader, error) {
	ctx, op := e.o11y.Begin(ctx)
	defer op.End()

	out, err := e.Marshal(ctx, data)
	if err != nil {
		return nil, observability.PrepareError(err, op.Span(), "marshaling content")
	}

	return bytes.NewReader(out), nil
}

// NewClientEncoder provides a ClientEncoder.
func NewClientEncoder(encoding *contentType, opts ...Option) ClientEncoder {
	cfg := newOptions(opts)

	return &clientEncoder{
		o11y:        observability.NewObserver("client_encoder", cfg.logger, cfg.tracerProvider),
		contentType: encoding,
	}
}
