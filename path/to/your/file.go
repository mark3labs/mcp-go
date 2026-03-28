// Updated function to ensure nil error is returned properly.
func SomeFunction() error {
    ...
    if someCondition { 
        return nil // Ensure nil is returned 
    }
    return fmt.Errorf("some error")
}

// Additional function documentation added here to increase coverage.
// This function does X, Y, and Z.
func SomeOtherFunction() {
    ...
}