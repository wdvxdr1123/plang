// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The main package permits running a specific version of Go.
package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

func init() {
	http.DefaultTransport = &userAgentTransport{http.DefaultTransport}
}

func runGo(root string) {
	gobin := filepath.Join(root, "bin", "go"+exe())
	cmd := exec.Command(gobin, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	newPath := filepath.Join(root, "bin")
	if p := os.Getenv("PATH"); p != "" {
		newPath += string(filepath.ListSeparator) + p
	}
	cmd.Env = dedupEnv(caseInsensitiveEnv, append(os.Environ(), "GOROOT="+root, "PATH="+newPath))

	handleSignals()

	if err := cmd.Run(); err != nil {
		// TODO: return the same exit status maybe.
		os.Exit(1)
	}
	os.Exit(0)
}

// getOS returns runtime.GOOS. It exists as a function just for lazy
// testing of the Windows zip path when running on Linux/Darwin.
func getOS() string {
	return runtime.GOOS
}

const caseInsensitiveEnv = runtime.GOOS == "windows"

func exe() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func goroot(version string) (string, error) {
	home, err := homedir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %v", err)
	}
	return filepath.Join(home, "sdk", version), nil
}

func homedir() (string, error) {
	// This could be replaced with os.UserHomeDir, but it was introduced too
	// recently, and we want this to work with go as packaged by Linux
	// distributions. Note that user.Current is not enough as it does not
	// prioritize $HOME. See also Issue 26463.
	switch getOS() {
	case "plan9":
		return "", fmt.Errorf("%q not yet supported", runtime.GOOS)
	case "windows":
		if dir := os.Getenv("USERPROFILE"); dir != "" {
			return dir, nil
		}
		return "", errors.New("can't find user home directory; %USERPROFILE% is empty")
	default:
		if dir := os.Getenv("HOME"); dir != "" {
			return dir, nil
		}
		if u, err := user.Current(); err == nil && u.HomeDir != "" {
			return u.HomeDir, nil
		}
		return "", errors.New("can't find user home directory; $HOME is empty")
	}
}

type userAgentTransport struct {
	rt http.RoundTripper
}

func (uat userAgentTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	version := runtime.Version()
	if strings.Contains(version, "devel") {
		// Strip the SHA hash and date. We don't want spaces or other tokens (see RFC2616 14.43)
		version = "devel"
	}
	r.Header.Set("User-Agent", "golang-x-build-version/"+version)
	return uat.rt.RoundTrip(r)
}

// dedupEnv returns a copy of env with any duplicates removed, in favor of
// later values.
// Items are expected to be on the normal environment "key=value" form.
// If caseInsensitive is true, the case of keys is ignored.
//
// This function is unnecessary when the binary is
// built with Go 1.9+, but keep it around for now until Go 1.8
// is no longer seen in the wild in common distros.
//
// This is copied verbatim from golang.org/x/build/envutil.Dedup at CL 10301
// (commit a91ae26).
func dedupEnv(caseInsensitive bool, env []string) []string {
	out := make([]string, 0, len(env))
	saw := map[string]int{} // to index in the array
	for _, kv := range env {
		eq := strings.Index(kv, "=")
		if eq < 1 {
			out = append(out, kv)
			continue
		}
		k := kv[:eq]
		if caseInsensitive {
			k = strings.ToLower(k)
		}
		if dupIdx, isDup := saw[k]; isDup {
			out[dupIdx] = kv
		} else {
			saw[k] = len(out)
			out = append(out, kv)
		}
	}
	return out
}

func handleSignals() {
	// Ensure that signals intended for the child process are not handled by
	// this process' runtime (e.g. SIGQUIT). See issue #36976.
	signal.Notify(make(chan os.Signal), signalsToIgnore...)
}
