package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitializeAndListenGuard(t *testing.T) {
	var output bytes.Buffer
	if err := run(context.Background(), []string{"init", "--config", filepath.Join(t.TempDir(), "hub.json")}, &output, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Read token: wbh_") || !strings.Contains(output.String(), "Write token: wbh_") {
		t.Fatal("missing one-time token output")
	}
	for _, address := range []string{"0.0.0.0:8090", ":8090", "example.com:8090", "[::]:8090"} {
		if err := validateListen(address, false); err == nil {
			t.Errorf("unguarded remote address: %s", address)
		}
	}
	for _, address := range []string{"127.0.0.1:8090", "[::1]:8090"} {
		if err := validateListen(address, false); err != nil {
			t.Error(err)
		}
	}
	if err := validateListen("0.0.0.0:8090", true); err != nil {
		t.Error(err)
	}
}
