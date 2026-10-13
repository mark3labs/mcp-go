package mcp

import (
	"encoding/json"
	"math"
	"math/big"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseIntegerJSONRegression(t *testing.T) {
	var request CallToolRequest
	require.NoError(t, json.Unmarshal([]byte(`{"params":{"name":"test","arguments":{"padded":"010","eight":"08","large":300,"bad":"abc","negative":-1}}}`), &request))
	assert.Equal(t, 10, ParseInt(request, "padded", 99))
	assert.Equal(t, 8, ParseInt(request, "eight", 99))
	assert.Equal(t, int8(99), ParseInt8(request, "large", 99))
	assert.Equal(t, 99, ParseInt(request, "bad", 99))
	assert.Equal(t, uint8(99), ParseUInt8(request, "negative", 99))
}

type integerTestSigned int

type integerConversionCase[T comparable] struct {
	name  string
	value any
	want  T
}

func testIntegerWrapper[T comparable](t *testing.T, parse func(CallToolRequest, string, T) T, convert func(string) T, bits int, signed bool, fallback T) {
	t.Helper()
	one := convert("1")
	zero := convert("0")
	tests := []integerConversionCase[T]{
		{"padded", "010", convert("10")}, {"eight", "08", convert("8")},
		{"whitespace", " \t42\n", convert("42")}, {"zero", "0", zero},
		{"true", true, one}, {"false", false, zero},
		{"json padded", json.Number("010"), convert("10")},
		{"json whitespace", json.Number(" 42 "), convert("42")},
		{"float fraction", 12.9, convert("12")}, {"float32 fraction", float32(12.9), convert("12")},
		{"negative zero", math.Copysign(0, -1), zero},
	}
	for _, v := range []any{int(42), int8(42), int16(42), int32(42), int64(42), uint(42), uint8(42), uint16(42), uint32(42), uint64(42), float32(42), float64(42)} {
		tests = append(tests, integerConversionCase[T]{"numeric", v, convert("42")})
	}
	pointer := 42
	for _, v := range []any{nil, "", "abc", "0x10", "0o10", "0b10", "1_0", "42.0", "1e2", "42junk", json.Number("abc"), json.Number("1.0"), json.Number("1e2"), map[string]any{}, []int{1}, &pointer, integerTestSigned(42), json.Number(""), json.Number("0x10"), math.NaN(), math.Inf(1), math.Inf(-1), float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1)), math.MaxFloat64, -math.MaxFloat64} {
		tests = append(tests, integerConversionCase[T]{"invalid", v, fallback})
	}
	upperBits := bits
	if signed {
		upperBits--
	}
	upper := new(big.Int).Lsh(big.NewInt(1), uint(upperBits))
	max := new(big.Int).Sub(new(big.Int).Set(upper), big.NewInt(1)).String()
	for _, v := range []any{max, json.Number(max), upper.String(), json.Number(upper.String())} {
		want := fallback
		if v == max || v == json.Number(max) {
			want = convert(max)
		}
		tests = append(tests, integerConversionCase[T]{"upper endpoint", v, want})
	}
	upperFloat := math.Ldexp(1, upperBits)
	tests = append(tests, integerConversionCase[T]{"exclusive float upper", upperFloat, fallback},
		integerConversionCase[T]{"float32 upper", float32(upperFloat), fallback},
		integerConversionCase[T]{"float above upper", math.Nextafter(upperFloat, math.Inf(1)), fallback})
	neighbor := math.Trunc(math.Nextafter(upperFloat, 0))
	tests = append(tests, integerConversionCase[T]{"float below upper", math.Nextafter(upperFloat, 0), convert(strconv.FormatFloat(neighbor, 'f', 0, 64))})
	if signed {
		min := new(big.Int).Neg(new(big.Int).Set(upper))
		for _, v := range []any{min.String(), json.Number(min.String()), -upperFloat} {
			tests = append(tests, integerConversionCase[T]{"minimum", v, convert(min.String())})
		}
		tests = append(tests,
			integerConversionCase[T]{"below minimum", new(big.Int).Sub(new(big.Int).Set(min), big.NewInt(1)).String(), fallback},
			integerConversionCase[T]{"negative fraction", -12.9, convert("-12")},
			integerConversionCase[T]{"negative float32 fraction", float32(-12.9), convert("-12")},
			integerConversionCase[T]{"float32 minimum", float32(-upperFloat), convert(min.String())},
			integerConversionCase[T]{"plus", "+42", convert("42")},
			integerConversionCase[T]{"unsigned overflow", uint64(math.MaxInt64) + 1, fallback},
		)
		above := math.Trunc(math.Nextafter(-upperFloat, 0))
		tests = append(tests, integerConversionCase[T]{"float above minimum", math.Nextafter(-upperFloat, 0), convert(strconv.FormatFloat(above, 'f', 0, 64))})
		below := math.Nextafter(-upperFloat, math.Inf(-1))
		want := fallback
		if math.Trunc(below) == -upperFloat {
			want = convert(min.String())
		}
		tests = append(tests, integerConversionCase[T]{"float below minimum", below, want})
	} else {
		for _, v := range []any{"-1", "+1", json.Number("-1"), json.Number("+1"), int64(-1), -0.5, float32(-0.5)} {
			tests = append(tests, integerConversionCase[T]{"negative or signed", v, fallback})
		}
	}
	// Numeric endpoints exercise the cross-sign and narrowing paths independently of strings.
	if signed {
		maxValue, _ := strconv.ParseInt(max, 10, 64)
		tests = append(tests,
			integerConversionCase[T]{"numeric max", maxValue, convert(max)},
			integerConversionCase[T]{"unsigned max", uint64(maxValue), convert(max)},
			integerConversionCase[T]{"numeric above max", uint64(maxValue) + 1, fallback},
			integerConversionCase[T]{"numeric min", -maxValue - 1, convert(strconv.FormatInt(-maxValue-1, 10))},
		)
		if bits < 64 {
			tests = append(tests, integerConversionCase[T]{"numeric below min", -maxValue - 2, fallback})
		}
	} else {
		maxValue, _ := strconv.ParseUint(max, 10, 64)
		tests = append(tests, integerConversionCase[T]{"numeric max", maxValue, convert(max)})
		if bits < 64 {
			tests = append(tests, integerConversionCase[T]{"numeric above max", maxValue + 1, fallback})
		}
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := CallToolRequest{}
			request.Params.Arguments = map[string]any{"value": tt.value}
			assert.Equal(t, tt.want, parse(request, "value", fallback))
		})
	}
	for _, args := range []any{nil, map[string]any{}, []any{}, "not a map"} {
		request := CallToolRequest{}
		request.Params.Arguments = args
		assert.Equal(t, fallback, parse(request, "value", fallback))
	}
}

func TestParseIntegerWrappers(t *testing.T) {
	t.Run("int", func(t *testing.T) {
		testIntegerWrapper(t, ParseInt, func(s string) int {
			v, err := strconv.ParseInt(s, 10, strconv.IntSize)
			require.NoError(t, err)
			return int(v)
		}, strconv.IntSize, true, int(99))
	})
	t.Run("int8", func(t *testing.T) {
		testIntegerWrapper(t, ParseInt8, func(s string) int8 {
			v, err := strconv.ParseInt(s, 10, 8)
			require.NoError(t, err)
			return int8(v)
		}, 8, true, int8(99))
	})
	t.Run("int16", func(t *testing.T) {
		testIntegerWrapper(t, ParseInt16, func(s string) int16 {
			v, err := strconv.ParseInt(s, 10, 16)
			require.NoError(t, err)
			return int16(v)
		}, 16, true, int16(99))
	})
	t.Run("int32", func(t *testing.T) {
		testIntegerWrapper(t, ParseInt32, func(s string) int32 {
			v, err := strconv.ParseInt(s, 10, 32)
			require.NoError(t, err)
			return int32(v)
		}, 32, true, int32(99))
	})
	t.Run("int64", func(t *testing.T) {
		testIntegerWrapper(t, ParseInt64, func(s string) int64 {
			v, err := strconv.ParseInt(s, 10, 64)
			require.NoError(t, err)
			return int64(v)
		}, 64, true, int64(99))
	})
	t.Run("uint", func(t *testing.T) {
		testIntegerWrapper(t, ParseUInt, func(s string) uint {
			v, err := strconv.ParseUint(s, 10, strconv.IntSize)
			require.NoError(t, err)
			return uint(v)
		}, strconv.IntSize, false, uint(99))
	})
	t.Run("uint8", func(t *testing.T) {
		testIntegerWrapper(t, ParseUInt8, func(s string) uint8 {
			v, err := strconv.ParseUint(s, 10, 8)
			require.NoError(t, err)
			return uint8(v)
		}, 8, false, uint8(99))
	})
	t.Run("uint16", func(t *testing.T) {
		testIntegerWrapper(t, ParseUInt16, func(s string) uint16 {
			v, err := strconv.ParseUint(s, 10, 16)
			require.NoError(t, err)
			return uint16(v)
		}, 16, false, uint16(99))
	})
	t.Run("uint32", func(t *testing.T) {
		testIntegerWrapper(t, ParseUInt32, func(s string) uint32 {
			v, err := strconv.ParseUint(s, 10, 32)
			require.NoError(t, err)
			return uint32(v)
		}, 32, false, uint32(99))
	})
	t.Run("uint64", func(t *testing.T) {
		testIntegerWrapper(t, ParseUInt64, func(s string) uint64 {
			v, err := strconv.ParseUint(s, 10, 64)
			require.NoError(t, err)
			return uint64(v)
		}, 64, false, uint64(99))
	})
}
