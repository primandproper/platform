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
