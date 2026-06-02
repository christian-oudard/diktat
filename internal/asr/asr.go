// Package asr runs moonshine-tiny ONNX inference and decodes tokens.
package asr

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// moonshine-tiny architecture.
const (
	numLayers       = 6
	numKVHeads      = 8
	headDim         = 36
	bosToken  int64 = 1
	eosToken  int64 = 2
	maxLen          = 192
)

// Model holds open encoder + decoder sessions and the tokenizer.
type Model struct {
	encoder              *ort.DynamicAdvancedSession
	encInNames           []string
	encOutNames          []string
	decoder              *ort.DynamicAdvancedSession
	decInNames           []string
	decOutNames          []string
	tok                  *Tokenizer
}

var initOnce sync.Once
var initErr error

func initORT(libPath string) error {
	initOnce.Do(func() {
		ort.SetSharedLibraryPath(libPath)
		initErr = ort.InitializeEnvironment()
	})
	return initErr
}

// LoadModel opens the moonshine ONNX sessions from modelDir, which must
// contain encoder.onnx, decoder.onnx, tokenizer.json.
func LoadModel(modelDir, ortLibPath string) (*Model, error) {
	if err := initORT(ortLibPath); err != nil {
		return nil, fmt.Errorf("init onnxruntime: %w", err)
	}
	encPath := filepath.Join(modelDir, "encoder.onnx")
	decPath := filepath.Join(modelDir, "decoder.onnx")
	tokPath := filepath.Join(modelDir, "tokenizer.json")

	encIn, encOut, err := ort.GetInputOutputInfo(encPath)
	if err != nil {
		return nil, fmt.Errorf("encoder IO: %w", err)
	}
	encInNames := names(encIn)
	encOutNames := names(encOut)
	encSess, err := ort.NewDynamicAdvancedSession(encPath, encInNames, encOutNames, nil)
	if err != nil {
		return nil, fmt.Errorf("encoder session: %w", err)
	}

	decIn, decOut, err := ort.GetInputOutputInfo(decPath)
	if err != nil {
		encSess.Destroy()
		return nil, fmt.Errorf("decoder IO: %w", err)
	}
	decInNames := names(decIn)
	decOutNames := names(decOut)
	decSess, err := ort.NewDynamicAdvancedSession(decPath, decInNames, decOutNames, nil)
	if err != nil {
		encSess.Destroy()
		return nil, fmt.Errorf("decoder session: %w", err)
	}

	tok, err := LoadTokenizer(tokPath)
	if err != nil {
		encSess.Destroy()
		decSess.Destroy()
		return nil, fmt.Errorf("tokenizer: %w", err)
	}

	return &Model{
		encoder:     encSess,
		encInNames:  encInNames,
		encOutNames: encOutNames,
		decoder:     decSess,
		decInNames:  decInNames,
		decOutNames: decOutNames,
		tok:         tok,
	}, nil
}

// Close releases the sessions.
func (m *Model) Close() {
	m.encoder.Destroy()
	m.decoder.Destroy()
}

// Transcribe runs encoder + autoregressive decoder and returns the decoded
// text. audio is 16kHz mono float32 in [-1, 1].
func (m *Model) Transcribe(audio []float32) (string, error) {
	hidden, err := m.encode(audio)
	if err != nil {
		return "", err
	}
	defer hidden.Destroy()
	ids, err := m.decode(hidden)
	if err != nil {
		return "", err
	}
	return m.tok.Decode(ids), nil
}

func (m *Model) encode(audio []float32) (*ort.Tensor[float32], error) {
	audioTensor, err := ort.NewTensor(ort.NewShape(1, int64(len(audio))), audio)
	if err != nil {
		return nil, err
	}
	defer audioTensor.Destroy()

	inValues := make([]ort.Value, len(m.encInNames))
	var cleanup []func()
	defer func() {
		for _, fn := range cleanup {
			fn()
		}
	}()
	for i, n := range m.encInNames {
		switch n {
		case "input_values":
			inValues[i] = audioTensor
		case "attention_mask":
			mask := make([]int64, len(audio))
			for j := range mask {
				mask[j] = 1
			}
			t, err := ort.NewTensor(ort.NewShape(1, int64(len(audio))), mask)
			if err != nil {
				return nil, err
			}
			cleanup = append(cleanup, func() { t.Destroy() })
			inValues[i] = t
		default:
			return nil, fmt.Errorf("unexpected encoder input %q", n)
		}
	}

	outValues := make([]ort.Value, len(m.encOutNames))
	if err := m.encoder.Run(inValues, outValues); err != nil {
		return nil, err
	}
	hidden := outValues[0].(*ort.Tensor[float32])
	for _, v := range outValues[1:] {
		v.Destroy()
	}
	return hidden, nil
}

func (m *Model) decode(hidden *ort.Tensor[float32]) ([]int64, error) {
	currentKV := map[string]*ort.Tensor[float32]{}
	for layer := 0; layer < numLayers; layer++ {
		for _, side := range []string{"decoder", "encoder"} {
			for _, kind := range []string{"key", "value"} {
				name := fmt.Sprintf("past_key_values.%d.%s.%s", layer, side, kind)
				t, err := ort.NewEmptyTensor[float32](ort.NewShape(0, numKVHeads, 1, headDim))
				if err != nil {
					destroyAll(currentKV)
					return nil, err
				}
				currentKV[name] = t
			}
		}
	}
	defer destroyAll(currentKV)

	tokens := []int64{bosToken}
	for step := 0; step < maxLen; step++ {
		useCache := step > 0
		inValues := make([]ort.Value, len(m.decInNames))
		var stepCleanup []func()
		for i, n := range m.decInNames {
			switch {
			case n == "input_ids":
				ids := []int64{tokens[len(tokens)-1]}
				if !useCache {
					ids = []int64{bosToken}
				}
				t, err := ort.NewTensor(ort.NewShape(1, int64(len(ids))), ids)
				if err != nil {
					return nil, err
				}
				stepCleanup = append(stepCleanup, func() { t.Destroy() })
				inValues[i] = t
			case n == "encoder_hidden_states":
				inValues[i] = hidden
			case n == "encoder_attention_mask":
				shape := hidden.GetShape()
				mask := make([]int64, shape[1])
				for j := range mask {
					mask[j] = 1
				}
				t, err := ort.NewTensor(ort.NewShape(1, shape[1]), mask)
				if err != nil {
					return nil, err
				}
				stepCleanup = append(stepCleanup, func() { t.Destroy() })
				inValues[i] = t
			case n == "use_cache_branch":
				t, err := ort.NewTensor(ort.NewShape(1), []bool{useCache})
				if err != nil {
					return nil, err
				}
				stepCleanup = append(stepCleanup, func() { t.Destroy() })
				inValues[i] = t
			case strings.HasPrefix(n, "past_key_values."):
				inValues[i] = currentKV[n]
			default:
				return nil, fmt.Errorf("unexpected decoder input %q", n)
			}
		}

		outValues := make([]ort.Value, len(m.decOutNames))
		if err := m.decoder.Run(inValues, outValues); err != nil {
			for _, fn := range stepCleanup {
				fn()
			}
			return nil, err
		}
		for _, fn := range stepCleanup {
			fn()
		}

		logitsTensor := outValues[0].(*ort.Tensor[float32])
		logits := logitsTensor.GetData()
		shape := logitsTensor.GetShape()
		vocab := shape[len(shape)-1]
		next := argmax(logits[int64(len(logits))-vocab:])
		logitsTensor.Destroy()

		for i, name := range m.decOutNames[1:] {
			tensor := outValues[1+i].(*ort.Tensor[float32])
			pastName := strings.Replace(name, "present.", "past_key_values.", 1)
			if !useCache || strings.Contains(pastName, ".decoder.") {
				if old := currentKV[pastName]; old != nil {
					old.Destroy()
				}
				currentKV[pastName] = tensor
			} else {
				tensor.Destroy()
			}
		}

		tokens = append(tokens, next)
		if next == eosToken {
			break
		}
	}
	return tokens, nil
}

func argmax(xs []float32) int64 {
	best := 0
	for i, x := range xs {
		if x > xs[best] {
			best = i
		}
	}
	return int64(best)
}

func names(infos []ort.InputOutputInfo) []string {
	out := make([]string, len(infos))
	for i, info := range infos {
		out[i] = info.Name
	}
	return out
}

func destroyAll(m map[string]*ort.Tensor[float32]) {
	for _, t := range m {
		if t != nil {
			t.Destroy()
		}
	}
}
