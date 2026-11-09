package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"backend/fleet-agent/internal/config"
	"backend/fleet-agent/internal/state"
)

const maxConfigureInput = 64 << 10
const configureTimeout = 15 * time.Second

type configureRequest struct {
	AccessToken        *string `json:"accessToken"`
	LocalAPIToken      *string `json:"localAPIToken"`
	InstallationID     *string `json:"installationID"`
	ComposeProjectName *string `json:"composeProjectName"`
	DesiredConnected   *bool   `json:"desiredConnected"`
	ClearConnection    bool    `json:"clearConnection"`
}

type configureResponse struct {
	Configured             bool   `json:"configured"`
	LocalAPITokenGenerated bool   `json:"localAPITokenGenerated"`
	LocalAPIToken          string `json:"localAPIToken,omitempty"`
	DataDir                string `json:"dataDir"`
}

func runConfigure(arguments []string) error {
	flags := flag.NewFlagSet("configure", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dataDirFlag := flags.String("data-dir", "", "Fleet Agent data directory")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("configure accepts JSON on stdin; access tokens must not be command-line arguments")
	}

	var request configureRequest
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, maxConfigureInput+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return fmt.Errorf("decode configure JSON from stdin: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("configure input must contain one JSON value")
	}

	dataDir, err := config.ResolveDataDir(*dataDirFlag)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), configureTimeout)
	defer cancel()
	store, err := state.Open(ctx, dataDir)
	if err != nil {
		return err
	}
	defer store.Close()
	persisted, _, err := store.LoadConfig(ctx)
	if err != nil {
		return err
	}
	if err := mergeConfigureRequest(&persisted, request); err != nil {
		return err
	}
	generatedLocalToken := ""
	if len(persisted.LocalTokenHash) == 0 {
		generatedLocalToken, err = generateLocalAPIToken()
		if err != nil {
			return err
		}
		persisted.LocalTokenHash = config.HashLocalToken(generatedLocalToken)
	}
	candidate := config.Config{
		HTTPAddr:           config.DefaultHTTPAddr,
		DataDir:            dataDir,
		AccessToken:        persisted.AccessToken,
		DesiredConnected:   persisted.DesiredConnected,
		LocalTokenHash:     persisted.LocalTokenHash,
		InstallationID:     persisted.InstallationID,
		ComposeProjectName: persisted.ComposeProjectName,
	}
	if err := config.ApplyConnectionToken(&candidate); err != nil {
		return err
	}
	if err := candidate.Validate(); err != nil {
		return err
	}
	if err := store.SaveConfig(ctx, config.Persisted{
		CenterURL:          candidate.CenterURL,
		FleetNodeID:        candidate.FleetNodeID,
		AccessToken:        candidate.AccessToken,
		DesiredConnected:   candidate.DesiredConnected,
		LocalTokenHash:     candidate.LocalTokenHash,
		InstallationID:     candidate.InstallationID,
		ComposeProjectName: candidate.ComposeProjectName,
		LastConnectedAt:    persisted.LastConnectedAt,
	}); err != nil {
		return err
	}
	response := configureResponse{
		Configured:             candidate.CenterConfigured(),
		LocalAPITokenGenerated: generatedLocalToken != "",
		DataDir:                dataDir,
	}
	if generatedLocalToken != "" {
		response.LocalAPIToken = generatedLocalToken
	}
	return json.NewEncoder(os.Stdout).Encode(response)
}

func mergeConfigureRequest(persisted *config.Persisted, request configureRequest) error {
	if request.ClearConnection && request.AccessToken != nil {
		return errors.New("clearConnection cannot be combined with accessToken")
	}
	if request.ClearConnection {
		persisted.CenterURL = ""
		persisted.FleetNodeID = ""
		persisted.AccessToken = ""
		persisted.DesiredConnected = false
	} else if request.AccessToken != nil {
		accessToken := strings.TrimSpace(*request.AccessToken)
		if accessToken == "" {
			return errors.New("accessToken must not be empty; use clearConnection to remove the connection")
		}
		persisted.AccessToken = accessToken
		persisted.DesiredConnected = true
	}
	if request.DesiredConnected != nil {
		if *request.DesiredConnected && strings.TrimSpace(persisted.AccessToken) == "" {
			return errors.New("desiredConnected requires an existing accessToken")
		}
		persisted.DesiredConnected = *request.DesiredConnected
	}

	if request.LocalAPIToken != nil {
		localToken := strings.TrimSpace(*request.LocalAPIToken)
		if len(localToken) < 32 || len(localToken) > 512 {
			return errors.New("localAPIToken must contain 32-512 characters")
		}
		persisted.LocalTokenHash = config.HashLocalToken(localToken)
	}
	if request.InstallationID != nil {
		installationID := strings.TrimSpace(*request.InstallationID)
		if installationID == "" {
			return errors.New("installationID must not be empty when provided")
		}
		persisted.InstallationID = installationID
	}
	if request.ComposeProjectName != nil {
		persisted.ComposeProjectName = strings.TrimSpace(*request.ComposeProjectName)
	}
	return nil
}

func generateLocalAPIToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate local API token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
