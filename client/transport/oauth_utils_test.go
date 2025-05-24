package transport

import (
	"fmt"
	"testing"
)

func TestGenerateRandomString(t *testing.T) {
	// Test generating strings of different lengths
	lengths := []int{10, 32, 64, 128}
	for _, length := range lengths {
		t.Run(fmt.Sprintf("Length_%d", length), func(t *testing.T) {
			str, err := GenerateRandomString(length)
			if err != nil {
				t.Fatalf("Failed to generate random string: %v", err)
			}
			if len(str) != length {
				t.Errorf("Expected string of length %d, got %d", length, len(str))
			}

			// Generate another string to ensure they're different
			str2, err := GenerateRandomString(length)
			if err != nil {
				t.Fatalf("Failed to generate second random string: %v", err)
			}
			if str == str2 {
				t.Errorf("Generated identical random strings: %s", str)
			}
		})
	}
}

func TestGenerateCodeVerifierAndChallenge(t *testing.T) {
	// Generate a code verifier
	verifier, err := GenerateCodeVerifier()
	if err != nil {
		t.Fatalf("Failed to generate code verifier: %v", err)
	}

	// Verify the length (should be 64 characters)
	if len(verifier) != 64 {
		t.Errorf("Expected code verifier of length 64, got %d", len(verifier))
	}

	// Generate a code challenge
	challenge := GenerateCodeChallenge(verifier)

	// Verify the challenge is not empty
	if challenge == "" {
		t.Errorf("Generated empty code challenge")
	}

	// Generate another verifier and challenge to ensure they're different
	verifier2, _ := GenerateCodeVerifier()
	challenge2 := GenerateCodeChallenge(verifier2)

	if verifier == verifier2 {
		t.Errorf("Generated identical code verifiers: %s", verifier)
	}
	if challenge == challenge2 {
		t.Errorf("Generated identical code challenges: %s", challenge)
	}

	// Verify the same verifier always produces the same challenge
	challenge3 := GenerateCodeChallenge(verifier)
	if challenge != challenge3 {
		t.Errorf("Same verifier produced different challenges: %s and %s", challenge, challenge3)
	}
}

func TestGenerateState(t *testing.T) {
	// Generate a state parameter
	state, err := GenerateState()
	if err != nil {
		t.Fatalf("Failed to generate state: %v", err)
	}

	// Verify the length (should be 32 characters)
	if len(state) != 32 {
		t.Errorf("Expected state of length 32, got %d", len(state))
	}

	// Generate another state to ensure they're different
	state2, _ := GenerateState()
	if state == state2 {
		t.Errorf("Generated identical states: %s", state)
	}
}