package guard

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/VBenevides/pig-plugins/internal/lancet"
)

// ModelInfo describes the local LANCET model and runtime for status output.
type ModelInfo struct {
	Directory string
	// Installed is true when every model file has its pinned size.
	Installed bool
	// Verified is true when every model file also has its pinned SHA-256.
	Verified bool
	// Problem says why the model is not installed.
	Problem string
	// Runtime is the ONNX Runtime library that would be loaded; RuntimeProblem is set when there is none.
	Runtime        string
	RuntimeProblem string
}

// Lancet is the local scoring service the command and the gate use.
type Lancet interface {
	Scorer
	// Model inspects the installed model and runtime. It hashes the files, so it takes a moment.
	Model() ModelInfo
	// Loaded reports whether the model is in memory.
	Loaded() bool
	// Setup downloads and verifies whatever is missing. It returns what it installed, an empty list when
	// everything was current.
	Setup(ctx context.Context, progress func(what string)) (installed []string, err error)
	// Release frees the model; a later Score loads it again.
	Release() error
}

// Local is the Lancet implementation that runs the pinned model in this process.
type Local struct {
	agentDir string
	getenv   func(string) string
	options  lancet.InstallOptions

	mu      sync.Mutex
	loaded  *lancet.Classifier
	setupMu sync.Mutex
}

// NewLocal creates a service that keeps the model under agentDir. options are the production zero value except
// in tests.
func NewLocal(agentDir string, getenv func(string) string, options lancet.InstallOptions) *Local {
	return &Local{agentDir: agentDir, getenv: getenv, options: options}
}

// classifier loads the model on first use. A failed load is not cached, so setup can repair it without a restart.
func (l *Local) classifier() (*lancet.Classifier, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.loaded != nil {
		return l.loaded, nil
	}
	dir := lancet.ModelDirectory(l.agentDir)
	if state := lancet.ModelStateOf(dir); !state.Installed {
		return nil, fmt.Errorf("the LANCET model is %s; run /smart-approve-lancet lancet setup", state.Problem)
	}
	library, err := lancet.LibraryPath(l.agentDir, l.getenv)
	if err != nil {
		return nil, err
	}
	c, err := lancet.Load(dir, library)
	if err != nil {
		return nil, err
	}
	l.loaded = c
	return c, nil
}

// Score scores one bash command. Any failure is an error and the gate blocks.
func (l *Local) Score(ctx context.Context, command string) (lancet.Result, error) {
	c, err := l.classifier()
	if err != nil {
		return lancet.Result{}, err
	}
	switch result, err := c.Score(ctx, command, "bash"); {
	case err != nil:
		return lancet.Result{}, err
	case result.Classification != lancet.Risky && result.Classification != lancet.NotFlagged && result.Classification != lancet.Review:
		return lancet.Result{}, errors.New("LANCET returned an invalid verdict")
	default:
		return result, nil
	}
}

// Model inspects the installed files.
func (l *Local) Model() ModelInfo {
	dir := lancet.ModelDirectory(l.agentDir)
	info := ModelInfo{Directory: dir}
	state := lancet.ModelStateOf(dir)
	info.Installed, info.Problem = state.Installed, state.Problem
	info.Verified = state.Installed && lancet.ModelVerified(dir)
	if library, err := lancet.LibraryPath(l.agentDir, l.getenv); err != nil {
		info.RuntimeProblem = err.Error()
	} else {
		info.Runtime = library
	}
	return info
}

// Loaded reports whether the model is in memory.
func (l *Local) Loaded() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.loaded != nil
}

// Setup installs the model and, unless LANCET_ORT_LIBRARY names a library, the pinned ONNX Runtime.
func (l *Local) Setup(ctx context.Context, progress func(what string)) ([]string, error) {
	if !l.setupMu.TryLock() {
		return nil, errors.New("another setup is already running")
	}
	defer l.setupMu.Unlock()
	options := l.options
	var installed []string
	options.OnProgress = func(phase string) {
		if phase == "download" && progress != nil {
			progress("downloading")
		}
	}
	model, err := lancet.InstallModel(ctx, l.agentDir, options)
	if err != nil {
		return installed, err
	}
	if model.Reason == "downloaded" {
		installed = append(installed, "model")
	}
	if l.getenv(lancet.LibraryEnv) == "" {
		runtime, err := lancet.InstallRuntime(ctx, l.agentDir, options)
		if err != nil {
			return installed, err
		}
		if runtime.Reason == "downloaded" {
			installed = append(installed, "ONNX Runtime")
		}
	}
	return installed, nil
}

// Release frees the loaded model.
func (l *Local) Release() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.loaded == nil {
		return nil
	}
	c := l.loaded
	l.loaded = nil
	return c.Close()
}
