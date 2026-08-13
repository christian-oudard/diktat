module github.com/christian-oudard/diktat

go 1.24

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/gen2brain/malgo v0.11.25
	github.com/handy-computer/transcribe.cpp/bindings/go v0.0.0
)

replace github.com/handy-computer/transcribe.cpp/bindings/go => ../transcribe-cpp/bindings/go
