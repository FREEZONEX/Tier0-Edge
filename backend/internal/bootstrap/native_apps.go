package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"backend/internal/domain/appinstall"
	"backend/internal/repo"
)

func (a *App) initializeNativeApps(ctx context.Context) error {
	c := a.Config.NativeApps
	if c.Root == "" {
		return nil
	}
	credentials, err := appinstall.NewCredentials(c.Root, c.CredentialKey, fmt.Sprintf("http://127.0.0.1:%d", a.Config.Port), a.UnsV2)
	if err != nil {
		return err
	}
	runtime, err := appinstall.New(ctx, appinstall.Options{Root: c.Root, Tools: appinstall.Tools{Node: c.Node, NPMCLI: c.NPMCLI, PNPMCLI: c.PNPMCLI}, Repository: repo.NewAppInstallationRepo(ctx), Environment: credentials.Environment, Validate: func(ctx context.Context, id string) error {
		app, err := repo.NewUnsMetaRepo(ctx).Get(ctx, id, false, "app")
		if err != nil {
			return err
		}
		if app.MountID != nil {
			return errors.New("mounted App definitions cannot be installed as local Apps")
		}
		var definition struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(app.Data, &definition) != nil || definition.Status == "disabled" {
			return errors.New("App definition is unavailable or disabled")
		}
		return nil
	}})
	if err != nil {
		return err
	}
	a.NativeApps = runtime
	a.NativeAppCredentials = credentials
	management, err := appinstall.NewManagement(ctx, runtime, credentials, a.UnsV2, a.Asset, c.Root, a.SourceStream)
	if err != nil {
		runtime.Close()
		return err
	}
	a.NativeAppManagement = management
	return nil
}
