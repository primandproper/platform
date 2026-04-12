package database

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/shoenig/test"
)

func Test_timeFromNullTime(T *testing.T) {
	T.Parallel()

	T.Run("with valid time", func(t *testing.T) {
		t.Parallel()

		now := time.Now()
		nt := sql.NullTime{Time: now, Valid: true}

		test.EqOp(t, now, TimeFromNullTime(nt))
	})

	T.Run("with invalid time", func(t *testing.T) {
		t.Parallel()

		nt := sql.NullTime{Valid: false}

		test.True(t, TimeFromNullTime(nt).IsZero())
	})
}

func Test_timePointerFromNullTime(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		expected := time.Now()
		actual := TimePointerFromNullTime(sql.NullTime{Time: expected, Valid: true})

		test.EqOp(t, expected, *actual)
	})

	T.Run("with invalid value", func(t *testing.T) {
		t.Parallel()

		actual := TimePointerFromNullTime(sql.NullTime{Time: time.Now(), Valid: false})

		test.Nil(t, actual)
	})
}

func Test_stringPointerFromNullString(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		expected := t.Name()
		actual := StringPointerFromNullString(sql.NullString{String: expected, Valid: true})

		test.NotNil(t, actual)
		test.EqOp(t, expected, *actual)
	})

	T.Run("with invalid value", func(t *testing.T) {
		t.Parallel()

		actual := StringPointerFromNullString(sql.NullString{String: t.Name(), Valid: false})

		test.Nil(t, actual)
	})
}

func Test_stringFromNullString(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		input := sql.NullString{String: t.Name(), Valid: true}
		test.EqOp(t, input.String, StringFromNullString(input))
	})

	T.Run("with invalid value", func(t *testing.T) {
		t.Parallel()

		input := sql.NullString{}
		test.EqOp(t, "", StringFromNullString(input))
	})
}

func Test_nullStringFromString(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		expected := sql.NullString{String: t.Name(), Valid: true}
		test.EqOp(t, expected, NullStringFromString(t.Name()))
	})
}

func Test_nullStringFromStringPointer(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		expected := sql.NullString{String: t.Name(), Valid: true}
		test.EqOp(t, expected, NullStringFromStringPointer(new(t.Name())))
	})

	T.Run("with nil value", func(t *testing.T) {
		t.Parallel()

		expected := sql.NullString{String: ""}
		test.EqOp(t, expected, NullStringFromStringPointer(nil))
	})
}

func Test_nullTimeFromTime(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		exampleTime := time.Now()
		expected := sql.NullTime{Time: exampleTime, Valid: true}
		test.EqOp(t, expected, NullTimeFromTime(exampleTime))
	})
}

func Test_nullTimeFromTimePointer(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		exampleTime := time.Now()
		expected := sql.NullTime{Time: exampleTime, Valid: true}
		test.EqOp(t, expected, NullTimeFromTimePointer(new(exampleTime)))
	})

	T.Run("with nil value", func(t *testing.T) {
		t.Parallel()

		expected := sql.NullTime{}
		test.EqOp(t, expected, NullTimeFromTimePointer(nil))
	})
}

func Test_nullInt32FromUint8Pointer(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		expected := sql.NullInt32{Int32: 123, Valid: true}
		test.EqOp(t, expected, NullInt32FromUint8Pointer(new(uint8(expected.Int32))))
	})

	T.Run("with nil value", func(t *testing.T) {
		t.Parallel()

		expected := sql.NullInt32{}
		test.EqOp(t, expected, NullInt32FromUint8Pointer(nil))
	})
}

func Test_nullInt32FromUint16Pointer(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		expected := sql.NullInt32{Int32: 123, Valid: true}
		test.EqOp(t, expected, NullInt32FromUint16Pointer(new(uint16(expected.Int32))))
	})

	T.Run("with nil value", func(t *testing.T) {
		t.Parallel()

		expected := sql.NullInt32{}
		test.EqOp(t, expected, NullInt32FromUint16Pointer(nil))
	})
}

func Test_nullInt32FromUint16(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		expected := sql.NullInt32{Int32: 123, Valid: true}
		test.EqOp(t, expected, NullInt32FromUint16(uint16(expected.Int32)))
	})
}

func Test_nullBoolFromBool(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		expected := sql.NullBool{Bool: true, Valid: true}
		test.EqOp(t, expected, NullBoolFromBool(true))
	})
}

func Test_nullBoolFromBoolPointer(T *testing.T) {
	T.Parallel()

	T.Run("with non-nil pointer", func(t *testing.T) {
		t.Parallel()

		b := true
		result := NullBoolFromBoolPointer(&b)

		test.True(t, result.Valid)
		test.True(t, result.Bool)
	})

	T.Run("with nil pointer", func(t *testing.T) {
		t.Parallel()

		result := NullBoolFromBoolPointer(nil)

		test.False(t, result.Valid)
	})
}

func Test_boolFromNullBool(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		input := sql.NullBool{Bool: true, Valid: true}
		test.EqOp(t, input.Bool, BoolFromNullBool(input))
	})

	T.Run("with invalid value", func(t *testing.T) {
		t.Parallel()

		input := sql.NullBool{Bool: true, Valid: false}
		test.False(t, BoolFromNullBool(input))
	})
}

func Test_nullInt32FromInt32Pointer(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		expected := sql.NullInt32{Int32: 123, Valid: true}
		test.EqOp(t, expected, NullInt32FromInt32Pointer(new(expected.Int32)))
	})

	T.Run("with nil value", func(t *testing.T) {
		t.Parallel()

		expected := sql.NullInt32{}
		test.EqOp(t, expected, NullInt32FromInt32Pointer(nil))
	})
}

func Test_nullInt32FromUint32Pointer(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		expected := sql.NullInt32{Int32: 123, Valid: true}
		test.EqOp(t, expected, NullInt32FromUint32Pointer(new(uint32(expected.Int32))))
	})

	T.Run("with nil value", func(t *testing.T) {
		t.Parallel()

		expected := sql.NullInt32{}
		test.EqOp(t, expected, NullInt32FromUint32Pointer(nil))
	})
}

func Test_int32PointerFromNullInt32(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		input := sql.NullInt32{Int32: 123, Valid: true}
		test.Eq(t, new(input.Int32), Int32PointerFromNullInt32(input))
	})

	T.Run("with invalid value", func(t *testing.T) {
		t.Parallel()

		input := sql.NullInt32{Int32: 123, Valid: false}
		test.Nil(t, Int32PointerFromNullInt32(input))
	})
}

func Test_float32PointerFromNullString(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		input := sql.NullString{String: "1.23", Valid: true}
		test.Eq(t, new(float32(1.23)), Float32PointerFromNullString(input))
	})

	T.Run("with invalid value", func(t *testing.T) {
		t.Parallel()

		input := sql.NullString{String: "1.23", Valid: false}
		test.Nil(t, Float32PointerFromNullString(input))
	})
}

func Test_float64PointerFromNullString(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		input := sql.NullString{String: "1.23", Valid: true}
		test.Eq(t, new(1.23), Float64PointerFromNullString(input))
	})

	T.Run("with invalid value", func(t *testing.T) {
		t.Parallel()

		input := sql.NullString{String: "1.23", Valid: false}
		test.Nil(t, Float64PointerFromNullString(input))
	})
}

func Test_stringFromFloat32(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		value := float32(1.23)
		test.EqOp(t, "1.23", StringFromFloat32(value))
	})
}

func Test_float32FromString(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, float32(1.23), Float32FromString("1.23"))
	})

	T.Run("with invalid value", func(t *testing.T) {
		t.Parallel()

		test.Zero(t, Float32FromString(t.Name()))
	})
}

func Test_float32FromNullString(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		input := sql.NullString{String: "1.23", Valid: true}
		test.EqOp(t, float32(1.23), Float32FromNullString(input))
	})

	T.Run("with invalid value", func(t *testing.T) {
		t.Parallel()

		input := sql.NullString{String: "1.23", Valid: false}
		test.Zero(t, Float32FromNullString(input))
	})
}

func Test_nullStringFromFloat32Pointer(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		value := float32(1.23)
		expected := sql.NullString{String: fmt.Sprintf("%v", value), Valid: true}
		test.EqOp(t, expected, NullStringFromFloat32Pointer(new(value)))
	})

	T.Run("with nil value", func(t *testing.T) {
		t.Parallel()

		expected := sql.NullString{}
		test.EqOp(t, expected, NullStringFromFloat32Pointer(nil))
	})
}

func Test_nullStringFromFloat32(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		value := float32(1.23)
		expected := sql.NullString{String: fmt.Sprintf("%v", value), Valid: true}
		test.EqOp(t, expected, NullStringFromFloat32(value))
	})
}

func Test_stringFromFloat64(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		value := float64(1.23)
		test.EqOp(t, "1.23", StringFromFloat64(value))
	})
}

func Test_nullStringFromFloat64Pointer(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		value := float64(1.23)
		expected := sql.NullString{String: fmt.Sprintf("%v", value), Valid: true}
		test.EqOp(t, expected, NullStringFromFloat64Pointer(new(value)))
	})

	T.Run("with nil value", func(t *testing.T) {
		t.Parallel()

		expected := sql.NullString{}
		test.EqOp(t, expected, NullStringFromFloat64Pointer(nil))
	})
}

func Test_nullInt64FromUint32Pointer(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		expected := sql.NullInt64{Int64: 123, Valid: true}
		test.EqOp(t, expected, NullInt64FromUint32Pointer(new(uint32(expected.Int64))))
	})

	T.Run("with nil value", func(t *testing.T) {
		t.Parallel()

		expected := sql.NullInt64{}
		test.EqOp(t, expected, NullInt64FromUint32Pointer(nil))
	})
}

func Test_uint16PointerFromNullInt32(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		input := sql.NullInt32{Int32: 123, Valid: true}
		test.Eq(t, new(uint16(input.Int32)), Uint16PointerFromNullInt32(input))
	})

	T.Run("with invalid value", func(t *testing.T) {
		t.Parallel()

		input := sql.NullInt32{Int32: 123, Valid: false}
		test.Nil(t, Uint16PointerFromNullInt32(input))
	})
}

func Test_uint32PointerFromNullInt32(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		input := sql.NullInt32{Int32: 123, Valid: true}
		test.Eq(t, new(uint32(input.Int32)), Uint32PointerFromNullInt32(input))
	})

	T.Run("with invalid value", func(t *testing.T) {
		t.Parallel()

		input := sql.NullInt32{Int32: 123, Valid: false}
		test.Nil(t, Uint32PointerFromNullInt32(input))
	})
}

func Test_uint32PointerFromNullInt64(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		input := sql.NullInt64{Int64: 123, Valid: true}
		test.Eq(t, new(uint32(input.Int64)), Uint32PointerFromNullInt64(input))
	})

	T.Run("with invalid value", func(t *testing.T) {
		t.Parallel()

		input := sql.NullInt64{Int64: 123, Valid: false}
		test.Nil(t, Uint32PointerFromNullInt64(input))
	})
}
