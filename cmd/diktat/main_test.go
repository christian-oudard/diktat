package main

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	refreshBar = func() {}
	os.Exit(m.Run())
}
