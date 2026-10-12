package hashline

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// canonicalMutationPath also resolves existing ancestors of a not-yet-created
// target, so new-file writes through directory aliases use the same lock key.
func canonicalMutationPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	candidate := absolute
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		if _, statErr := os.Lstat(candidate); statErr == nil {
			return "", err // A dangling symlink is not a new file.
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			return "", statErr
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return "", err
		}
		missing = append(missing, filepath.Base(candidate))
		candidate = parent
	}
}

// WithMutationLock serializes cooperating in-process mutations of a canonical
// target. Callers must mutate the supplied target, not re-resolve an alias.
// This does not coordinate shell commands or unrelated processes.
func WithMutationLock(path string, mutate func(target string) error) error {
	return WithMutationLockContext(context.Background(), path, mutate)
}

func WithMutationLockContext(request Cancellation, path string, mutate func(target string) error) error {
	if err := request.Err(); err != nil {
		return err
	}
	target, err := canonicalMutationPath(path)
	if err != nil {
		return fmt.Errorf("resolve mutation target %s: %w", path, err)
	}
	unlock, err := lockPathContext(request, target)
	if err != nil {
		return fmt.Errorf("wait for mutation lock %s: %w", target, err)
	}
	defer unlock()
	return mutate(target)
}

func WriteSchema() map[string]any {
	return map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{"path", "content"},
		"properties": map[string]any{
			"path":    map[string]any{"type": "string", "minLength": 1},
			"content": map[string]any{"type": "string", "maxLength": MaxEditBytes},
		},
	}
}

const WriteDescription = "Write a UTF-8 text file using path and content. Creates parent directories and creates or explicitly replaces the file. Shares hashline edit's mutation coordinator and filesystem safety policy. New files are private (0600). Never use write to bypass a rejected edit; reread instead."

func Write(cwd string, params map[string]any) (Result, error) {
	return WriteContext(context.Background(), cwd, params)
}

func WriteContext(request Cancellation, cwd string, params map[string]any) (Result, error) {
	if err := request.Err(); err != nil {
		return Result{}, err
	}
	path, _ := params["path"].(string)
	if path == "" {
		return Result{}, errors.New("path must be a non-empty string.")
	}
	if err := rejectUnknownFields(params, "write request", "path", "content"); err != nil {
		return Result{}, fmt.Errorf("write rejected: %w", err)
	}
	content, ok := params["content"].(string)
	if !ok {
		return Result{}, errors.New("content must be a string.")
	}
	if len(content) > MaxEditBytes {
		return Result{}, editErrorf("Write exceeds %d bytes.", MaxEditBytes)
	}
	if IsBinary([]byte(content)) {
		return Result{}, editErrorf("Write content must be UTF-8 text without binary content or NUL bytes.")
	}
	parsed, err := ParseFileContext(request, content)
	if err != nil {
		return Result{}, err
	}
	if err := validateResultSize(parsed); err != nil {
		return Result{}, err
	}
	absolute, err := filepath.Abs(ResolveToolPath(path, cwd))
	if err != nil {
		return Result{}, err
	}
	var result Result
	err = WithMutationLockContext(request, absolute, func(target string) error {
		root, err := filepath.Abs(cwd)
		if err != nil {
			return err
		}
		root, err = filepath.EvalSymlinks(root)
		if err != nil {
			return fmt.Errorf("resolve working directory %s: %w", cwd, err)
		}
		relative, err := filepath.Rel(root, target)
		if err != nil {
			return err
		}
		outside := relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator))
		if target != absolute && outside {
			return editErrorf("Refusing to write %s: its path traverses a symlink outside the working directory.", absolute)
		}
		changed := true
		if _, err := os.Lstat(target); err == nil {
			snapshot, err := TakeSnapshotContext(request, target, root, MaxEditBytes)
			if err != nil {
				return err
			}
			changed = string(snapshot.Bytes) != content
			if changed {
				if err := ReplaceAtomicallyContext(request, snapshot, content, nil); err != nil {
					return err
				}
			}
		} else if errors.Is(err, fs.ErrNotExist) {
			if err := createTextFileContext(request, target, content); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("inspect write target %s: %w", target, err)
		}
		message := fmt.Sprintf("Wrote %d bytes to %s.", len(content), absolute)
		if !changed {
			if err := request.Err(); err != nil {
				return err
			}
			message = fmt.Sprintf("No change: %s already has the requested content.", absolute)
		}
		result = withVersion(Result{
			Text:    message,
			Details: map[string]any{"changed": changed},
		}, []byte(content))
		return nil
	})
	if err != nil {
		var committed *CommittedError
		if errors.As(err, &committed) {
			return Result{}, fmt.Errorf("write committed with post-commit failure: %w", err)
		}
		return Result{}, fmt.Errorf("write rejected: %w", err)
	}
	return result, nil
}

// A hard-link publication creates the destination only if absent. Rename would
// overwrite a file created by an external writer after our initial inspection.
func createTextFile(target, content string) (err error) {
	return createTextFileContext(context.Background(), target, content)
}

func createTextFileContext(request Cancellation, target, content string) (err error) {
	return createTextFileWithSync(request, target, content, syncDirectory)
}

func createTextFileWithSync(request Cancellation, target, content string, syncParent func(string) error) (err error) {
	if err := request.Err(); err != nil {
		return err
	}
	directory := filepath.Dir(target)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return fmt.Errorf("create parent directory for %s: %w", target, err)
	}
	if err := request.Err(); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".pig-plugins-write-*.tmp")
	if err != nil {
		return fmt.Errorf("prepare new file %s: %w", target, err)
	}
	temporary := file.Name()
	published := false
	cleaned := false
	defer func() {
		if file != nil {
			if closeErr := file.Close(); closeErr != nil {
				err = errors.Join(err, fmt.Errorf("close preparation file for %s: %w", target, closeErr))
			}
		}
		if !cleaned {
			if removeErr := os.Remove(temporary); removeErr != nil {
				cleanupErr := fmt.Errorf("cleanup new file %s: %w", target, removeErr)
				if published {
					cleanupErr = &CommittedError{Path: target, Err: cleanupErr}
				}
				err = errors.Join(err, cleanupErr)
			}
		}
	}()
	if _, err := checkSupportedMetadata(temporary); err != nil {
		return err
	}
	if err := writePreparedText(request, file, content); err != nil {
		return fmt.Errorf("write preparation file for %s: %w", target, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync new file %s: %w", target, err)
	}
	closing := file
	file = nil
	if err := closing.Close(); err != nil {
		return fmt.Errorf("close new file %s: %w", target, err)
	}
	if err := request.Err(); err != nil {
		return err
	}
	if err := os.Link(temporary, target); err != nil {
		return fmt.Errorf("publish new file %s without overwriting another writer: %w", target, err)
	}
	published = true
	if err := os.Remove(temporary); err != nil {
		return &CommittedError{Path: target, Err: fmt.Errorf("remove published preparation link: %w", err)}
	}
	cleaned = true
	if err := syncParent(directory); err != nil {
		return &CommittedError{Path: target, Err: err}
	}
	return nil
}
