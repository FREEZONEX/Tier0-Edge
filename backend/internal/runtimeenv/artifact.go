package runtimeenv

import (
	"backend/internal/product"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type Distribution struct {
	product.Identity
	ProductVersion    string `json:"productVersion"`
	CodeVersion       string `json:"codeVersion"`
	DataCompatibility struct {
		Format        int    `json:"format"`
		PostgresMajor string `json:"postgresMajor"`
		Metadata      string `json:"metadata"`
		Data          string `json:"data"`
	} `json:"dataCompatibility"`
}

// ReadArtifactIdentity asks the trusted installed binary for its compiled identity.
// trimpath removes linker flags from build info, so manifests/build-info cannot
// prove edition. Legacy binaries without this query fail closed.
func ReadArtifactIdentity(file string) (product.Identity, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, file, "--product-identity")
	hideIdentityQuery(command)
	raw, err := command.Output()
	if err != nil {
		return product.Identity{}, fmt.Errorf("installed binary cannot prove product identity; explicit legacy migration required: %w", err)
	}
	var id product.Identity
	if len(raw) > 4096 || json.Unmarshal(raw, &id) != nil || (id.Edition != "edge" && id.Edition != "enterprise") || id.Platform == "" || (id.Deployment != "native" && id.Deployment != "docker") {
		return id, fmt.Errorf("invalid compiled product identity")
	}
	return id, nil
}

func ReadDistribution(root string) (Distribution, error) {
	raw, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return Distribution{}, err
	}
	var manifest struct {
		Distribution Distribution `json:"distribution"`
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		return Distribution{}, err
	}
	return manifest.Distribution, nil
}

func ValidateDistribution(d Distribution) error {
	if d.Identity != Identity() {
		return fmt.Errorf("package identity does not match compiled product (%s/%s/%s)", Edition(), Identity().Platform, Mode())
	}
	if d.ProductVersion != BuildVersion || d.CodeVersion != BuildRevision || d.DataCompatibility.Format != 1 || d.DataCompatibility.PostgresMajor != "17" || d.DataCompatibility.Metadata != "metadata/1" || d.DataCompatibility.Data != "data/3" {
		return fmt.Errorf("package version or data compatibility contract mismatch")
	}
	return nil
}

// IdentityQuery is handled before loading config or starting any services.
func IdentityQuery() bool {
	if len(os.Args) != 2 || os.Args[1] != "--product-identity" {
		return false
	}
	_ = json.NewEncoder(os.Stdout).Encode(struct {
		product.Identity
		ProductVersion string `json:"productVersion"`
		CodeVersion    string `json:"codeVersion"`
	}{Identity(), BuildVersion, BuildRevision})
	return true
}
