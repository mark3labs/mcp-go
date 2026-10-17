package main

import "strconv"

func pow(n int, p int) int {
	result := 1
	for p > 0 {
		if (p & 1) == 1 {
			result *= n
		}
		n *= n
		p >>= 1
	}
	return result
}

// n < 10000 is 4 digits
var maxDigits = 4
var maxId = pow(10, maxDigits)

func isIdInRange(num int) bool {
	return num >= 0 && num < maxId
}

// getIdSuggestions returns 10 suggestions for a given number.
// It returns the current argument value appended with 0-9.
func getIdSuggestions(num int) []string {
	suggestions := make([]string, 0, 10)
	for i := range 10 {
		suggestions = append(suggestions, strconv.Itoa(num*10+i))
	}
	return suggestions
}

func getTotalIds(digits int) int {
	// Leftover digits represent the total number of choices.
	return pow(10, maxDigits-digits)
}

func hasMoreIds(digits int) bool {
	return digits < maxDigits-1
}
