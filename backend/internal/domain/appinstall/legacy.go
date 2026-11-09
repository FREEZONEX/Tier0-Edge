package appinstall

import (
	"errors"
	"strings"
)

// Only portable Node invocation metadata is translated. Container images,
// environments, ports, databases, roles and resource imports are never applied.
type legacyArtifact struct {
	Name     string `toml:"name"`
	Services []struct {
		Production struct {
			Run struct {
				Args []string `toml:"args"`
			} `toml:"run"`
			Build struct {
				Args []string `toml:"args"`
			} `toml:"build"`
			Health struct {
				Startup struct {
					Path string `toml:"path"`
				} `toml:"startup"`
			} `toml:"health"`
		} `toml:"production"`
	} `toml:"services"`
}

func (a legacyArtifact) apply(b *Bundle) error {
	if b.Name == "" {
		b.Name = a.Name
	}
	if len(a.Services) == 0 {
		return nil
	}
	if len(a.Services) != 1 {
		return errors.New("native App requires one portable Node service; multi-service artifact is unsupported")
	}
	p := a.Services[0].Production
	if b.Runtime.Entry == "" && b.Runtime.StartScript == "" && len(p.Run.Args) > 0 {
		manager, script, entry, err := portableInvocation(p.Run.Args, false)
		if err != nil {
			return err
		}
		b.Runtime.PackageManager = manager
		b.Runtime.StartScript = script
		b.Runtime.Entry = entry
	}
	if b.Runtime.BuildScript == "" && len(p.Build.Args) > 0 {
		manager, script, _, err := portableInvocation(p.Build.Args, true)
		if err != nil {
			return err
		}
		if b.Runtime.PackageManager != "" && b.Runtime.PackageManager != manager {
			return errors.New("artifact uses different build and run package managers")
		}
		b.Runtime.PackageManager = manager
		b.Runtime.BuildScript = script
	}
	if b.Runtime.HealthPath == "" {
		b.Runtime.HealthPath = p.Health.Startup.Path
	}
	return nil
}

func portableInvocation(args []string, build bool) (manager, script, entry string, err error) {
	if len(args) == 2 && args[0] == "node" && !build && !strings.HasPrefix(args[1], "-") {
		if _, e := safeBundlePath(args[1]); e == nil {
			return "", "", args[1], nil
		}
	}
	if len(args) == 3 && (args[0] == "npm" || args[0] == "pnpm") && args[1] == "run" && packageScriptName.MatchString(args[2]) {
		return args[0], args[2], "", nil
	}
	if len(args) == 2 && (args[0] == "npm" || args[0] == "pnpm") && args[1] == "start" && !build {
		return args[0], "start", "", nil
	}
	return "", "", "", errors.New("artifact command is not a portable Node entry or npm/pnpm run script; declare runtime.entry/buildScript/startScript in meta.json")
}
