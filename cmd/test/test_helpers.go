package test

import (
	"strings"
	"testing"
)

func AssertNoError(t *testing.T, err error) {
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}

func AssertError(t *testing.T, err error) {
	if err == nil {
		t.Error("Expected an error, got nil")
	}
}

func AssertEqual(t *testing.T, expected, actual interface{}) {
	if expected != actual {
		t.Errorf("Expected %v, got %v", expected, actual)
	}
}

func AssertNotEqual(t *testing.T, expected, actual interface{}) {
	if expected == actual {
		t.Errorf("Expected values to be different, both are %v", expected)
	}
}

func AssertStringContains(t *testing.T, str, substr string) {
	if !strings.Contains(str, substr) {
		t.Errorf("Expected string '%s' to contain '%s'", str, substr)
	}
}

func AssertStringNotContains(t *testing.T, output, contains string) {
	if strings.Contains(output, contains) {
		t.Errorf("Expected output to not contain %q, but it did", contains)
	}
}
