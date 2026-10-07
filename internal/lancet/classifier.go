package lancet

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"unicode/utf8"
)

// Classification is the verdict band of a scored command.
type Classification string

const (
	Risky      Classification = "risky"
	NotFlagged Classification = "not_flagged"
	Review     Classification = "review"
)

// Result is one verdict. Score is nil when the input was refused before scoring (Reason says why).
type Result struct {
	Classification Classification
	Score          *float64
	// Logit is the uncalibrated risk logit the bands are cut from; zero when Score is nil.
	Logit           float64
	ReviewThreshold float64
	RiskyThreshold  float64
	// Reason is empty for a plain verdict, "uncertainty-band" for Review from the model, or the refusal reason.
	Reason string
}

type metadata struct {
	Format string `json:"format"`
	Kind   string `json:"kind"`
	Input  struct {
		WindowTokens  int `json:"windowTokens"`
		OverlapTokens int `json:"overlapTokens"`
		MaxUTF8Bytes  int `json:"maxUtf8Bytes"`
	} `json:"input"`
	ReviewLogitThreshold float64 `json:"reviewLogitThreshold"`
	RiskyLogitThreshold  float64 `json:"riskyLogitThreshold"`
	ReviewThreshold      float64 `json:"reviewThreshold"`
	RiskyThreshold       float64 `json:"riskyThreshold"`
	Calibration          struct {
		Scale float64 `json:"scale"`
		Bias  float64 `json:"bias"`
	} `json:"calibration"`
}

const hiddenSize = 768

// encoder runs one token window through the CodeT5+ encoder and returns the hidden states, row-major
// [len(window), hiddenSize].
type encoder interface {
	encode(ctx context.Context, window []int64) ([]float32, error)
	close() error
}

// Classifier scores bash commands. It is safe for concurrent use; scores run one at a time.
type Classifier struct {
	meta    metadata
	tok     *byteLevelBPE
	enc     encoder
	head    []float32
	normEps float64
	bosID   int64
	eosID   int64

	mu sync.Mutex
}

// Load verifies every artifact in dir against the pinned release, then loads the tokenizer, the head and the
// ONNX encoder. libraryPath is the ONNX Runtime shared library (libonnxruntime.so.1.x).
func Load(dir, libraryPath string) (*Classifier, error) {
	files := map[string][]byte{}
	for _, name := range []string{"model.json", "vocab.json", "merges.txt", "head.json", "head.bin", "encoder-int8.onnx"} {
		data, err := readVerified(dir, name)
		if err != nil {
			return nil, err
		}
		files[name] = data
	}

	var meta metadata
	if err := json.Unmarshal(files["model.json"], &meta); err != nil {
		return nil, fmt.Errorf("parse model.json: %w", err)
	}
	if err := checkMetadata(meta); err != nil {
		return nil, err
	}
	tok, err := newByteLevelBPE(files["vocab.json"], files["merges.txt"])
	if err != nil {
		return nil, err
	}
	bos, err := tok.tokenID("<s>")
	if err != nil {
		return nil, err
	}
	eos, err := tok.tokenID("</s>")
	if err != nil {
		return nil, err
	}
	var headSpec struct {
		NormEps float64 `json:"normEps"`
	}
	if err := json.Unmarshal(files["head.json"], &headSpec); err != nil {
		return nil, fmt.Errorf("parse head.json: %w", err)
	}
	// The verified head.json pins the little-endian float32 C-order layout: projection, norm, two heads.
	raw := files["head.bin"]
	head := make([]float32, len(raw)/4)
	for i := range head {
		head[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	enc, err := newORTEncoder(libraryPath, files["encoder-int8.onnx"])
	if err != nil {
		return nil, err
	}
	return &Classifier{meta: meta, tok: tok, enc: enc, head: head, normEps: headSpec.NormEps,
		bosID: int64(bos), eosID: int64(eos)}, nil
}

func checkMetadata(m metadata) error {
	finite := func(values ...float64) bool {
		for _, v := range values {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return false
			}
		}
		return true
	}
	if m.Format != "semantic-windowed-1" || m.Kind != "codet5p-encoder-windowed" ||
		m.Input.WindowTokens != 512 || m.Input.OverlapTokens != 64 || m.Input.MaxUTF8Bytes != 8192 ||
		!finite(m.ReviewThreshold, m.RiskyThreshold, m.ReviewLogitThreshold, m.RiskyLogitThreshold,
			m.Calibration.Scale, m.Calibration.Bias) {
		return errors.New("unsupported LANCET model metadata")
	}
	return nil
}

// Close releases the native session.
func (c *Classifier) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enc.close()
}

func (c *Classifier) refusal(reason string) Result {
	return Result{Classification: Review, ReviewThreshold: c.meta.ReviewThreshold,
		RiskyThreshold: c.meta.RiskyThreshold, Reason: reason}
}

// tokenize returns token ids, or the reason the input needs review. It mirrors LancetNano.encode.
func (c *Classifier) tokenize(command, shell string) (ids []int, reason string, err error) {
	switch {
	case shell != "bash":
		return nil, "unsupported-shell", nil
	case strings.TrimFunc(command, pythonSpace) == "":
		return nil, "empty-command", nil
	case strings.ContainsRune(command, 0):
		return nil, "nul-byte", nil
	case !utf8.ValidString(command):
		return nil, "invalid-unicode", nil
	case len(command) > c.meta.Input.MaxUTF8Bytes:
		return nil, "raw-input-too-long", nil
	}
	ids, err = c.tok.encode(command)
	return ids, "", err
}

// Score classifies one command. Refused input (empty, NUL, too long, unsupported shell) returns a Review result
// with a Reason and no score. Model or runtime failure, or a cancelled ctx, returns an error: callers fail closed.
func (c *Classifier) Score(ctx context.Context, command, shell string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	ids, reason, err := c.tokenize(command, shell)
	if err != nil {
		return Result{}, err
	}
	if reason != "" {
		return c.refusal(reason), nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	total := make([]float64, hiddenSize)
	maximum := make([]float64, hiddenSize)
	for i := range maximum {
		maximum[i] = math.Inf(-1)
	}
	capacity := c.meta.Input.WindowTokens - 2
	previousEnd := 0
	for start := 0; start < len(ids); {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		end := min(len(ids), start+capacity)
		window := make([]int64, 0, end-start+2)
		window = append(window, c.bosID)
		for _, id := range ids[start:end] {
			window = append(window, int64(id))
		}
		window = append(window, c.eosID)

		hidden, err := c.enc.encode(ctx, window)
		if err != nil {
			return Result{}, err
		}
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if len(hidden) != len(window)*hiddenSize {
			return c.refusal("invalid-model-output"), nil
		}
		for token := previousEnd; token < end; token++ {
			offset := (token - start + 1) * hiddenSize
			for column := range hiddenSize {
				value := float64(hidden[offset+column])
				if math.IsNaN(value) || math.IsInf(value, 0) {
					return c.refusal("nonfinite-model-output"), nil
				}
				total[column] += value
				maximum[column] = math.Max(maximum[column], value)
			}
		}
		if end == len(ids) {
			break
		}
		previousEnd = end
		start = end - c.meta.Input.OverlapTokens
	}
	for column := range total {
		total[column] /= float64(len(ids))
	}

	projected := make([]float64, hiddenSize)
	mean := 0.0
	for row := range hiddenSize {
		value := 0.0
		offset := row * hiddenSize * 2
		for column := range hiddenSize {
			value += float64(c.head[offset+column]) * total[column]
			value += float64(c.head[offset+hiddenSize+column]) * maximum[column]
		}
		projected[row] = value
		mean += value
	}
	mean /= hiddenSize
	variance := 0.0
	for _, value := range projected {
		variance += (value - mean) * (value - mean)
	}
	divisor := math.Sqrt(variance/hiddenSize + c.normEps)
	normOffset := hiddenSize * hiddenSize * 2
	weightOffset := normOffset + hiddenSize*2
	logit := float64(c.head[weightOffset+hiddenSize*2])
	for column := range hiddenSize {
		normalized := (projected[column]-mean)/divisor*float64(c.head[normOffset+column]) +
			float64(c.head[normOffset+hiddenSize+column])
		logit += float64(c.head[weightOffset+column]) * normalized
	}
	if math.IsNaN(logit) || math.IsInf(logit, 0) {
		return c.refusal("nonfinite-model-output"), nil
	}

	calibrated := logit*c.meta.Calibration.Scale + c.meta.Calibration.Bias
	var score float64
	if calibrated >= 0 {
		score = 1 / (1 + math.Exp(-calibrated))
	} else {
		score = math.Exp(calibrated) / (1 + math.Exp(calibrated))
	}
	class := NotFlagged
	switch {
	case logit >= c.meta.RiskyLogitThreshold:
		class = Risky
	case logit >= c.meta.ReviewLogitThreshold:
		class = Review
	}
	result := Result{Classification: class, Score: &score, Logit: logit,
		ReviewThreshold: c.meta.ReviewThreshold, RiskyThreshold: c.meta.RiskyThreshold}
	if class == Review {
		result.Reason = "uncertainty-band"
	}
	return result, nil
}
