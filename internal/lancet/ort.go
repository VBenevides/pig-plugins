package lancet

import (
	"bytes"
	"context"
	"fmt"
	"runtime"

	ort "github.com/shota3506/onnxruntime-purego/onnxruntime"
)

// ortAPIVersion is the ONNX Runtime C API version the binding speaks. A newer libonnxruntime serves older API
// versions, so any library from 1.23 on loads (the Node implementation ships 1.30).
const ortAPIVersion = 23

// ortEncoder runs the encoder through libonnxruntime, loaded with dlopen (purego). PiG builds extensions with
// cgo disabled, so a cgo binding cannot be used here.
type ortEncoder struct {
	rt      *ort.Runtime
	env     *ort.Env
	session *ort.Session
	output  string
}

// newORTEncoder creates a CPU session with 4 intra-op threads, as the Node implementation does. The binding
// does not expose inter-op threads or the optimization level; both stay at ONNX Runtime defaults, and the
// parity test pins the resulting scores to the official ones.
func newORTEncoder(libraryPath string, model []byte) (*ortEncoder, error) {
	rt, err := ort.NewRuntime(libraryPath, ortAPIVersion)
	if err != nil {
		return nil, fmt.Errorf("ONNX Runtime could not be loaded from %s: %w", libraryPath, err)
	}
	env, err := rt.NewEnv("lancet", ort.LoggingLevelError)
	if err != nil {
		rt.Close()
		return nil, fmt.Errorf("create ONNX Runtime environment: %w", err)
	}
	session, err := rt.NewSessionFromReader(env, bytes.NewReader(model), &ort.SessionOptions{
		IntraOpNumThreads:  4,
		ExecutionProviders: []string{"CPUExecutionProvider"},
	})
	if err != nil {
		env.Close()
		rt.Close()
		return nil, fmt.Errorf("create LANCET encoder session: %w", err)
	}
	outputs := session.OutputNames()
	if len(outputs) == 0 {
		session.Close()
		env.Close()
		rt.Close()
		return nil, fmt.Errorf("LANCET encoder has no outputs")
	}
	return &ortEncoder{rt: rt, env: env, session: session, output: outputs[0]}, nil
}

// encode runs one window. ctx is checked before the run: a native run cannot be interrupted through this
// binding, and one window takes well under a second.
func (e *ortEncoder) encode(ctx context.Context, window []int64) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	shape := []int64{1, int64(len(window))}
	mask := make([]int64, len(window))
	for i := range mask {
		mask[i] = 1
	}
	inputIDs, err := ort.NewTensorValue(e.rt, window, shape)
	if err != nil {
		return nil, fmt.Errorf("create input_ids tensor: %w", err)
	}
	defer inputIDs.Close()
	attention, err := ort.NewTensorValue(e.rt, mask, shape)
	if err != nil {
		return nil, fmt.Errorf("create attention_mask tensor: %w", err)
	}
	defer attention.Close()

	outputs, err := e.session.Run(ctx, map[string]*ort.Value{"input_ids": inputIDs, "attention_mask": attention},
		ort.WithOutputNames(e.output))
	// ONNX Runtime reads the Go slices behind both tensors during the run.
	runtime.KeepAlive(window)
	runtime.KeepAlive(mask)
	if err != nil {
		return nil, fmt.Errorf("run LANCET encoder: %w", err)
	}
	out := outputs[e.output]
	defer out.Close()
	hidden, _, err := ort.GetTensorData[float32](out)
	if err != nil {
		return nil, fmt.Errorf("read LANCET encoder output %s: %w", e.output, err)
	}
	return hidden, nil
}

func (e *ortEncoder) close() error {
	e.session.Close()
	e.env.Close()
	return e.rt.Close()
}
