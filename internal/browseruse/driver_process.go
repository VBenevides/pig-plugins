package browseruse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

func driverEnvironment(environment []string) []string {
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, "MOZ_HEADLESS") {
			result = append(result, entry)
		}
	}
	return result
}

type driverProcess struct {
	command  *exec.Cmd
	done     chan struct{}
	waitErr  error
	lock     string
	stopOnce sync.Once
	stopErr  error
}

func (p *driverProcess) stop() error {
	p.stopOnce.Do(func() { p.stopErr = p.stopOwned() })
	return p.stopErr
}

func (p *driverProcess) stopOwned() (err error) {
	defer func() {
		select {
		case <-p.done:
			if removeErr := os.Remove(p.lock); removeErr != nil && !os.IsNotExist(removeErr) {
				err = errors.Join(err, removeErr)
			}
		default: // Keep the cache locked if the owned process could not stop.
		}
	}()
	select {
	case <-p.done:
		return p.waitErr
	default:
	}
	if signalErr := p.command.Process.Signal(os.Interrupt); signalErr != nil && !errors.Is(signalErr, os.ErrProcessDone) {
		if killErr := p.command.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			return fmt.Errorf("stop owned driver: %w", errors.Join(signalErr, killErr))
		}
	}
	select {
	case <-p.done:
		return nil
	case <-time.After(2 * time.Second):
	}
	if err := p.command.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("kill owned driver: %w", err)
	}
	select {
	case <-p.done:
		return nil
	case <-time.After(3 * time.Second):
		return fmt.Errorf("owned driver did not exit after termination")
	}
}

func launchDriver(ctx context.Context, binary string, mode Mode, lock string) (process *driverProcess, endpoint string, err error) {
	defer func() {
		if err == nil {
			return
		}
		if process != nil {
			err = errors.Join(err, process.stop())
			process = nil
			return
		}
		if removeErr := os.Remove(lock); removeErr != nil && !os.IsNotExist(removeErr) {
			err = errors.Join(err, removeErr)
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return nil, "", err
	}
	args := []string{"--port=" + strconv.Itoa(port), "--allowed-ips=127.0.0.1", "--silent"}
	if mode == Firefox {
		args = []string{"--host", "127.0.0.1", "--port", strconv.Itoa(port), "--log", "error"}
	}
	command := exec.Command(binary, args...)
	command.WaitDelay = time.Second
	// Visibility flags, not an inherited environment override, control Firefox.
	command.Env = driverEnvironment(os.Environ())
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return nil, "", fmt.Errorf("start owned browser driver: %w", err)
	}
	process = &driverProcess{command: command, done: make(chan struct{}), lock: lock}
	go func() { process.waitErr = command.Wait(); close(process.done) }()
	endpoint = "http://127.0.0.1:" + strconv.Itoa(port)
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client := &Client{endpoint: endpoint, http: &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error {
		return fmt.Errorf("driver readiness redirects are disabled")
	}}}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastCheckErr error
	for {
		select {
		case <-process.done:
			return process, "", errors.Join(errors.New("owned driver exited before readiness"), process.waitErr)
		case <-bounded.Done():
			return process, "", fmt.Errorf("wait for browser driver: %w", errors.Join(bounded.Err(), lastCheckErr))
		case <-ticker.C:
		}
		var status struct {
			Ready bool `json:"ready"`
		}
		lastCheckErr = client.request(bounded, "GET", "/status", nil, &status)
		if lastCheckErr == nil && status.Ready {
			select {
			case <-process.done:
				return process, "", errors.Join(errors.New("owned driver exited during readiness"), process.waitErr)
			default:
				return process, endpoint, nil
			}
		}
	}
}

// NewManagedClient checks the browser and prepares its driver on first use.
// Explicit endpoint clients created with NewClient remain externally managed.
func NewManagedClient(mode Mode, profile string) (*Client, error) {
	client, err := NewClient(mode, "http://127.0.0.1:1", profile)
	if err != nil {
		return nil, err
	}
	client.managed = true
	return client, nil
}

func (c *Client) prepareDriver(ctx context.Context) (err error) {
	setup, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	browser, err := detectBrowser(setup, c.mode)
	if err != nil {
		return err
	}
	cache := filepath.Join(filepath.Dir(c.profile), "drivers", driverCacheName(c.mode))
	if err := prepareProfile(cache); err != nil {
		return fmt.Errorf("prepare driver cache: %w", err)
	}
	lock := filepath.Join(cache, ".lock")
	if err := os.Mkdir(lock, 0700); err != nil {
		return fmt.Errorf("driver cache is in use or locked at %s. Close the other browser-use session. After a crash, verify its driver has stopped before removing the lock: %w", lock, err)
	}
	owned := false
	defer func() {
		if !owned {
			if cleanupErr := os.Remove(lock); cleanupErr != nil && !os.IsNotExist(cleanupErr) {
				err = errors.Join(err, cleanupErr)
			}
		}
	}()
	httpClient := driverHTTPClient()
	release, err := latestDriver(setup, httpClient, c.mode, browser.version, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return fmt.Errorf("resolve latest compatible browser driver: %w", err)
	}
	binary, err := installDriver(setup, httpClient, cache, release)
	if err != nil {
		return fmt.Errorf("install browser driver: %w", err)
	}
	owned = true // launchDriver now owns lock cleanup, including startup failures.
	process, endpoint, err := launchDriver(setup, binary, c.mode, lock)
	if err != nil {
		return err
	}
	if err := pruneDrivers(cache, release.version); err != nil {
		return errors.Join(err, process.stop())
	}
	c.driver = process
	c.endpoint = endpoint
	c.browserBinary = browser.path
	return nil
}
