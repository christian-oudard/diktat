package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("whisper-dictation-daemon: TODO")
	fmt.Println("ONNXRUNTIME_LIB:", os.Getenv("ONNXRUNTIME_LIB"))
	fmt.Println("MOONSHINE_MODEL_DIR:", os.Getenv("MOONSHINE_MODEL_DIR"))
	os.Exit(0)
}
