// Repeat: re-type the last transcription.
package main

import (
	"log"
	"os"

	"github.com/christian-oudard/diktat/internal/config"
	"github.com/christian-oudard/diktat/internal/ipc"
	"github.com/christian-oudard/diktat/internal/output"
)

func main() {
	raw, err := os.ReadFile(ipc.LastTextFile)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		log.Fatalf("read last text: %v", err)
	}
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		cfg = &config.Config{}
	}
	if err := output.Type(string(raw), cfg.PasteMethods); err != nil {
		log.Fatalf("type: %v", err)
	}
}
