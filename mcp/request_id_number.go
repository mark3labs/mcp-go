package mcp

import (
	"encoding/json"
	"math/big"
	"strconv"
	"strings"
)

// canonicalRequestNumber normalizes without expanding powers of ten.
func canonicalRequestNumber(value string) (string, *big.Int) {
	exponent := new(big.Int)
	if index := strings.IndexAny(value, "eE"); index >= 0 {
		exponent.SetString(value[index+1:], 10)
		value = value[:index]
	}
	negative := strings.HasPrefix(value, "-")
	value = strings.TrimPrefix(value, "-")
	if index := strings.IndexByte(value, '.'); index >= 0 {
		exponent.Sub(exponent, big.NewInt(int64(len(value)-index-1)))
		value = value[:index] + value[index+1:]
	}
	value = strings.TrimLeft(value, "0")
	if value == "" {
		return "0", new(big.Int)
	}
	trimmed := strings.TrimRight(value, "0")
	exponent.Add(exponent, big.NewInt(int64(len(value)-len(trimmed))))
	if negative {
		trimmed = "-" + trimmed
	}
	return trimmed, exponent
}

func integerRequestNumber(number json.Number) (any, bool) {
	coefficient, exponent := canonicalRequestNumber(number.String())
	if exponent.Sign() < 0 {
		return nil, false
	}
	if exponent.IsInt64() && exponent.Int64() <= 19 && int64(len(coefficient))+exponent.Int64() <= 20 {
		integer, err := strconv.ParseInt(coefficient+strings.Repeat("0", int(exponent.Int64())), 10, 64)
		if err == nil {
			return integer, true
		}
	}
	return number, true
}

func requestNumberKey(number json.Number) string {
	if value, integer := integerRequestNumber(number); integer {
		if value, ok := value.(int64); ok {
			return "int64:" + strconv.FormatInt(value, 10)
		}
	}
	coefficient, exponent := canonicalRequestNumber(number.String())
	return "number:" + coefficient + "e" + exponent.String()
}
