package appinstall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"backend/internal/appruntime"
)

type Tools struct {
	Node    string
	NPMCLI  string
	PNPMCLI string
}

func (t Tools) scriptArguments(manager, script string) ([]string, error) {
	cli := t.NPMCLI
	if manager == "pnpm" {
		cli = t.PNPMCLI
	}
	if cli == "" || !filepath.IsAbs(cli) {
		return nil, errors.New("bundled App package manager unavailable")
	}
	if info, err := os.Stat(cli); err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("bundled App package manager unavailable")
	}
	return []string{cli, "run", script}, nil
}

// Prepare runs dependency/build phases in the extracted candidate directory.
// It never writes platform DB/schema/Role/UNS resources or stops an old release.
func Prepare(ctx context.Context, tools Tools, release string, b Bundle, appID string, log io.Writer) error {
	directory := filepath.Join(release, filepath.FromSlash(b.Runtime.SourceDir))
	raw, err := os.ReadFile(filepath.Join(directory, "package.json"))
	if err != nil {
		return err
	}
	var pkg struct {
		Dependencies         map[string]any `json:"dependencies"`
		DevDependencies      map[string]any `json:"devDependencies"`
		OptionalDependencies map[string]any `json:"optionalDependencies"`
	}
	if json.Unmarshal(raw, &pkg) != nil {
		return errors.New("invalid App dependencies")
	}
	// Frontend frameworks also need the public prefix at build time. Build
	// scripts receive routing identity only, never the App's OpenAPI credential.
	spec := appruntime.Spec{Node: tools.Node, Directory: directory, Log: log, Environment: map[string]string{"NODE_ENV": "development", "npm_config_cache": filepath.Join(release, ".npm-cache"), "CI": "true", "APP_ID": appID, "APP_BASE_PATH": "/apps/" + appID}}
	if b.Runtime.DependencyMode == "bundled" {
		// Validate the supplied tree without installing, downloading or deleting it.
		// Missing dependencies must fail closed; never fall back to online install.
		spec.Environment["npm_config_offline"] = "true"
		if len(pkg.Dependencies)+len(pkg.DevDependencies)+len(pkg.OptionalDependencies) > 0 {
			info, err := os.Stat(filepath.Join(directory, "node_modules"))
			if err != nil || !info.IsDir() {
				return errors.New("bundled dependencies require node_modules in the App source directory")
			}
			args, err := tools.scriptArguments("npm", "start")
			if err != nil {
				return err
			}
			spec.Arguments = []string{args[0], "ls", "--all", "--offline", "--include=dev"}
			if err = appruntime.Run(ctx, spec); err != nil {
				return fmt.Errorf("bundled dependency tree is incomplete or incompatible: %w", err)
			}
		}
		if log != nil {
			_, _ = io.WriteString(log, "Using bundled node_modules; dependency installation skipped (offline).\n")
		}
	} else if len(pkg.Dependencies)+len(pkg.DevDependencies)+len(pkg.OptionalDependencies) > 0 {
		args, err := tools.scriptArguments(b.Runtime.PackageManager, "start")
		if err != nil {
			return err
		}
		cli := args[0]
		if b.Runtime.PackageManager == "pnpm" {
			spec.Arguments = []string{cli, "install", "--frozen-lockfile"}
		} else {
			action := "install"
			if _, err := os.Stat(filepath.Join(directory, "package-lock.json")); err == nil {
				action = "ci"
			}
			spec.Arguments = []string{cli, action, "--no-audit", "--no-fund", "--include=dev"}
		}
		if err = appruntime.Run(ctx, spec); err != nil {
			return err
		}
	}
	if b.Runtime.BuildScript != "" {
		spec.Arguments, err = tools.scriptArguments(b.Runtime.PackageManager, b.Runtime.BuildScript)
		if err != nil {
			return err
		}
		if err = appruntime.Run(ctx, spec); err != nil {
			return err
		}
	}
	return nil
}

func StartCandidate(ctx context.Context, tools Tools, release string, b Bundle, appID string, environment map[string]string, log io.Writer) (*appruntime.Process, int, error) {
	port, err := appruntime.AllocatePort()
	if err != nil {
		return nil, 0, err
	}
	env := map[string]string{}
	for k, v := range environment {
		env[k] = v
	}
	env["HOST"] = "127.0.0.1"
	env["PORT"] = strconv.Itoa(port)
	env["APP_BASE_PATH"] = "/apps/" + appID
	env["APP_ID"] = appID
	if b.Runtime.DependencyMode == "bundled" {
		env["npm_config_offline"] = "true"
	}
	spec := appruntime.Spec{Node: tools.Node, Directory: filepath.Join(release, filepath.FromSlash(b.Runtime.SourceDir)), Environment: env, Log: log}
	if b.Runtime.Entry != "" {
		spec.Arguments = []string{b.Runtime.Entry}
	} else {
		spec.Arguments, err = tools.scriptArguments(b.Runtime.PackageManager, b.Runtime.StartScript)
		if err != nil {
			return nil, 0, err
		}
	}
	p, err := appruntime.Start(spec)
	if err != nil {
		return nil, 0, err
	}
	if err = p.WaitHealthy(ctx, port, b.Runtime.HealthPath); err != nil {
		_ = p.Stop()
		return nil, 0, err
	}
	return p, port, nil
}
