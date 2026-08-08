package encoding

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"

	"github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/internal/cbormode"
	"github.com/primandproper/platform-go/v9/observability"
	"github.com/primandproper/platform-go/v9/observability/keys"
	"github.com/primandproper/platform-go/v9/panicking"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

const (
	// ContentTypeHeaderKey is the HTTP standard header name for content type.
	ContentTypeHeaderKey = "Content-type"

	o11yName = "server_encoder_decoder"

	contentTypeKey = "content_type"

	contentTypeXML   = "application/xml"
	contentTypeJSON  = "application/json"
	contentTypeTOML  = "application/toml"
	contentTypeYAML  = "application/yaml"
	contentTypeCBOR  = "application/cbor"
	contentTypeEmoji = "application/emoji"
)

var (
	defaultContentType = ContentTypeJSON
)

type (
	// ServerEncoderDecoder is an interface that allows for multiple implementations of HTTP response formats.
	ServerEncoderDecoder interface {
		RespondWithData(ctx context.Context, res http.ResponseWriter, val any)
		EncodeResponseWithStatus(ctx context.Context, res http.ResponseWriter, val any, statusCode int)
		DecodeRequest(ctx context.Context, req *http.Request, dest any) error
		DecodeBytes(ctx context.Context, payload []byte, dest any) error
		MustEncode(ctx context.Context, v any) []byte
		MustEncodeJSON(ctx context.Context, v any) []byte
	}

	// serverEncoderDecoder is our concrete implementation of EncoderDecoder.
	serverEncoderDecoder struct {
		o11y        observability.Observer
		panicker    panicking.Panicker
		contentType ContentType
	}

	decoder interface {
		Decode(v any) error
	}
)

type tomlDecoder struct {
	reader io.Reader
}

func newTomlDecoder(reader io.Reader) decoder {
	return &tomlDecoder{reader: reader}
}

func (t *tomlDecoder) Decode(v any) error {
	x, err := io.ReadAll(t.reader)
	if err != nil {
		return err
	}

	return toml.Unmarshal(x, v)
}

// DecodeBytes decodes bytes into values.
func (e *serverEncoderDecoder) DecodeBytes(ctx context.Context, data []byte, dest any) error {
	_, op := e.o11y.Begin(ctx, observability.WithValue(keys.LengthKey, len(data)))
	defer op.End()

	var d decoder
	switch e.contentType {
	case ContentTypeXML:
		d = xml.NewDecoder(bytes.NewReader(data))
	case ContentTypeTOML:
		d = newTomlDecoder(bytes.NewReader(data))
	case ContentTypeYAML:
		d = yaml.NewDecoder(bytes.NewReader(data))
	case ContentTypeCBOR:
		d = cbormode.NewDecoder(bytes.NewReader(data))
	case ContentTypeEmoji:
		d = newEmojiDecoder(bytes.NewReader(data))
	default:
		dec := json.NewDecoder(bytes.NewReader(data))

		// Unknown fields are rejected rather than ignored, so a typo'd or stale
		// field in a payload surfaces as a decode error instead of a zero value.
		dec.DisallowUnknownFields()

		d = dec
	}

	return d.Decode(dest)
}

type emojiDecoder struct {
	r io.Reader
}

func newEmojiDecoder(r io.Reader) decoder {
	return &emojiDecoder{r: r}
}

func (e *emojiDecoder) Decode(v any) error {
	encodedContent, err := io.ReadAll(e.r)
	if err != nil {
		return err
	}

	return unmarshalEmoji(encodedContent, v)
}

// encodeResponse encodes responses.
func (e *serverEncoderDecoder) encodeResponse(ctx context.Context, res http.ResponseWriter, v any, statusCode int) {
	_, op := e.o11y.Begin(ctx, observability.WithValue(keys.ResponseStatusKey, statusCode))
	defer op.End()

	// choose the encoder from the configured content type, not the writer's pre-set header,
	// so a configured encoder is honored even when the handler never sets a header.
	res.Header().Set(ContentTypeHeaderKey, e.contentType.String())
	res.WriteHeader(statusCode)

	out, err := marshalFuncFor(e.contentType)(v)
	if err != nil {
		op.Acknowledge(err, "encoding response")

		return
	}

	if _, err = res.Write(out); err != nil {
		op.Acknowledge(err, "writing response")
	}
}

func (e *serverEncoderDecoder) MustEncodeJSON(ctx context.Context, v any) []byte {
	_, op := e.o11y.Begin(ctx)
	defer op.End()

	out, err := json.Marshal(v)
	if err != nil {
		e.panicker.Panic(errors.Wrap(err, "encoding JSON content"))
	}

	return out
}

// MustEncode encodes data or else.
func (e *serverEncoderDecoder) MustEncode(ctx context.Context, v any) []byte {
	_, op := e.o11y.Begin(ctx)
	defer op.End()

	out, err := marshalFuncFor(e.contentType)(v)
	if err != nil {
		e.panicker.Panic(errors.Wrapf(err, "encoding %s content", e.contentType))
	}

	return out
}

// RespondWithData encodes successful responses with data.
func (e *serverEncoderDecoder) RespondWithData(ctx context.Context, res http.ResponseWriter, v any) {
	ctx, op := e.o11y.Begin(ctx)
	defer op.End()

	e.encodeResponse(ctx, res, v, http.StatusOK)
}

// EncodeResponseWithStatus encodes responses and writes the provided status to the response.
func (e *serverEncoderDecoder) EncodeResponseWithStatus(ctx context.Context, res http.ResponseWriter, v any, statusCode int) {
	ctx, op := e.o11y.Begin(ctx)
	defer op.End()

	e.encodeResponse(ctx, res, v, statusCode)
}

// DecodeRequest decodes request bodies into values.
func (e *serverEncoderDecoder) DecodeRequest(ctx context.Context, req *http.Request, v any) error {
	_, op := e.o11y.Begin(ctx)
	defer op.End()

	var d decoder
	switch contentTypeFromRequestHeader(req.Header.Get(ContentTypeHeaderKey)) {
	case ContentTypeXML:
		d = xml.NewDecoder(req.Body)
	case ContentTypeTOML:
		d = newTomlDecoder(req.Body)
	case ContentTypeYAML:
		d = yaml.NewDecoder(req.Body)
	case ContentTypeCBOR:
		d = cbormode.NewDecoder(req.Body)
	case ContentTypeEmoji:
		d = newEmojiDecoder(req.Body)
	default:
		dec := json.NewDecoder(req.Body)

		// Unknown fields are rejected rather than ignored, so a typo'd or stale
		// field in a payload surfaces as a decode error instead of a zero value.
		dec.DisallowUnknownFields()

		d = dec
	}

	defer func() {
		if err := req.Body.Close(); err != nil {
			op.Logger().Error("closing request body", err)
		}
	}()

	return d.Decode(v)
}

// NewServerEncoderDecoder provides a ServerEncoderDecoder.
func NewServerEncoderDecoder(contentType ContentType, opts ...Option) ServerEncoderDecoder {
	cfg := newOptions(opts)

	return &serverEncoderDecoder{
		// An encoder/decoder speaks one content type for its whole life, and every
		// operation below is about it, so it is stated once here. Previously only
		// DecodeBytes recorded it, which is exactly the "set at some call sites and
		// forgotten at the rest" case this constructor exists for.
		o11y: observability.NewObserverWithValues(o11yName, cfg.logger, cfg.tracerProvider,
			map[string]any{contentTypeKey: contentType.String()}),
		panicker:    panicking.NewProductionPanicker(),
		contentType: contentType,
	}
}
