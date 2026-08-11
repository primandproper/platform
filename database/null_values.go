package database

import (
	"database/sql"
	"strconv"
	"time"
)

func TimeFromNullTime(nt sql.NullTime) time.Time {
	if nt.Valid {
		return nt.Time
	}

	return time.Time{}
}

func TimePointerFromNullTime(nt sql.NullTime) *time.Time {
	if nt.Valid {
		return &nt.Time
	}

	return nil
}

func StringPointerFromNullString(nt sql.NullString) *string {
	if nt.Valid {
		return &nt.String
	}

	return nil
}

func StringFromNullString(nt sql.NullString) string {
	if nt.Valid {
		return nt.String
	}

	return ""
}

func NullStringFromString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: true}
}

func NullStringFromStringPointer(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}

	return sql.NullString{String: *s, Valid: true}
}

func NullTimeFromTime(t time.Time) sql.NullTime {
	return sql.NullTime{Time: t, Valid: true}
}

func NullTimeFromTimePointer(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}

	return sql.NullTime{Time: *t, Valid: true}
}

func NullInt32FromUint8Pointer(i *uint8) sql.NullInt32 {
	if i == nil {
		return sql.NullInt32{}
	}

	return sql.NullInt32{Int32: int32(*i), Valid: true}
}

func NullInt32FromUint16Pointer(i *uint16) sql.NullInt32 {
	if i == nil {
		return sql.NullInt32{}
	}

	return sql.NullInt32{Int32: int32(*i), Valid: true}
}

func NullInt32FromUint16(i uint16) sql.NullInt32 {
	return sql.NullInt32{Int32: int32(i), Valid: true}
}

func NullBoolFromBool(b bool) sql.NullBool {
	return sql.NullBool{Bool: b, Valid: true}
}

func NullBoolFromBoolPointer(b *bool) sql.NullBool {
	if b == nil {
		return sql.NullBool{Valid: false}
	}
	return sql.NullBool{Bool: *b, Valid: true}
}

func BoolFromNullBool(b sql.NullBool) bool {
	if b.Valid {
		return b.Bool
	}

	return false
}

func NullInt32FromInt32Pointer(i *int32) sql.NullInt32 {
	if i == nil {
		return sql.NullInt32{}
	}

	return sql.NullInt32{Int32: *i, Valid: true}
}

func NullInt32FromUint32Pointer(i *uint32) sql.NullInt32 {
	if i == nil {
		return sql.NullInt32{}
	}

	return sql.NullInt32{Int32: int32(*i), Valid: true}
}

func Int32PointerFromNullInt32(i sql.NullInt32) *int32 {
	if i.Valid {
		return &i.Int32
	}

	return nil
}

func Float32PointerFromNullString(f sql.NullString) *float32 {
	if f.Valid {
		if parsedFloat, err := strconv.ParseFloat(f.String, 64); err == nil {
			return new(float32(parsedFloat))
		}
	}

	return nil
}

func Float64PointerFromNullString(f sql.NullString) *float64 {
	if f.Valid {
		if parsedFloat, err := strconv.ParseFloat(f.String, 64); err == nil {
			return &parsedFloat
		}
	}

	return nil
}

func StringFromFloat32(f float32) string {
	return strconv.FormatFloat(float64(f), 'f', -1, 32)
}

func Float32FromString(s string) float32 {
	if parsedFloat, err := strconv.ParseFloat(s, 64); err == nil {
		return float32(parsedFloat)
	}

	return 0
}

func Float32FromNullString(s sql.NullString) float32 {
	if s.Valid {
		return Float32FromString(s.String)
	}

	return 0
}

func NullStringFromFloat32Pointer(f *float32) sql.NullString {
	if f == nil {
		return sql.NullString{}
	}

	return sql.NullString{String: StringFromFloat32(*f), Valid: true}
}

func NullStringFromFloat32(f float32) sql.NullString {
	return sql.NullString{String: StringFromFloat32(f), Valid: true}
}

func StringFromFloat64(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func NullStringFromFloat64Pointer(f *float64) sql.NullString {
	if f == nil {
		return sql.NullString{}
	}

	return sql.NullString{String: StringFromFloat64(*f), Valid: true}
}

func NullInt64FromUint32Pointer(f *uint32) sql.NullInt64 {
	if f == nil {
		return sql.NullInt64{}
	}

	return sql.NullInt64{Int64: int64(*f), Valid: true}
}

func Uint16PointerFromNullInt32(f sql.NullInt32) *uint16 {
	if f.Valid {
		return new(uint16(f.Int32))
	}

	return nil
}

func Uint32PointerFromNullInt32(f sql.NullInt32) *uint32 {
	if f.Valid {
		return new(uint32(f.Int32))
	}

	return nil
}

func Uint32PointerFromNullInt64(f sql.NullInt64) *uint32 {
	if f.Valid {
		return new(uint32(f.Int64))
	}

	return nil
}

// CoerceTime normalizes whatever a driver hands back for a timestamp read as
// `any`, reporting whether it recognized one.
//
// Timestamps are scanned as `any` rather than sql.NullTime because the drivers
// disagree. pgx and go-sql-driver return a time.Time, but modernc's SQLite
// driver stores a bound time.Time as Go's own String() rendering, and an
// aggregate over such a column loses the declared DATETIME affinity — so it
// comes back as a plain string that sql.NullTime refuses outright.
//
// A NULL reports false, and callers treat that as "no value" rather than as the
// zero time: an empty backlog is not a row created at the epoch.
func CoerceTime(v any) (time.Time, bool) {
	var s string

	switch typed := v.(type) {
	case nil:
		return time.Time{}, false
	case time.Time:
		return typed, true
	case string:
		s = typed
	case []byte:
		s = string(typed)
	default:
		return time.Time{}, false
	}

	// Go's String() layout comes first: it is what the SQLite path actually
	// produces, and the others are here so a driver change does not silently
	// zero the value.
	for _, layout := range []string{
		"2006-01-02 15:04:05.999999999 -0700 MST",
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	} {
		if parsed, parseErr := time.Parse(layout, s); parseErr == nil {
			return parsed, true
		}
	}

	return time.Time{}, false
}

// BlobOrNil maps an empty encoding to a SQL NULL rather than an empty blob.
//
// "No value" and "an empty value" mean the same thing in every column in this
// module that holds an encoded payload — no request, no failure map, no
// snapshot — and storing two renderings of it would make the round trip depend
// on which call site wrote the row: one reader gets nil back and another gets a
// zero-length slice, from rows that were written to mean the same thing.
func BlobOrNil(b []byte) any {
	if len(b) == 0 {
		return nil
	}

	return b
}

// CursorOrder reports the ORDER BY direction and the comparison operator a
// keyset-paginated read uses for a given sort direction.
//
// It is one function because the two halves have to agree and nothing checks
// that they do. A descending page that kept "id > cursor" reads the wrong side
// of the boundary: the first page comes back, and every page after it skips
// straight past the rows the caller asked for. That failure produces no error
// and no empty result — just a listing quietly missing its middle.
func CursorOrder(descending bool) (direction, comparison string) {
	if descending {
		return "DESC", " < "
	}

	return "ASC", " > "
}
