// Repeat: re-type the last transcription.
package main

import (
	"log"
	"os"

	"github.com/christian-oudard/whisper_dictation/internal/config"
	"github.com/christian-oudard/whisper_dictation/internal/output"
)

const lastTextFile = "/tmp/whisper-dictation-last"

func main() {
	raw, err := os.ReadFile(lastTextFile)
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
