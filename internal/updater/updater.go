package updater

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/vivaui/viva/update"
	"markstack/internal/buildinfo"
)

var ErrNotConfigured = errors.New("updates are not configured for this build")

type Config struct {
	Version, Channel, FeedURL, PublicKey, Directory string
	HTTP                                            *http.Client
}

type Client struct {
	client     *update.Client
	version    string
	directory  string
	executable string
	install    func(update.InstallOptions, func()) error
}

type Offer struct {
	Version          string
	FullSize         int64
	PatchSize        int64
	owner            *Client
	version          string
	full, patchAsset update.Asset
	patch            update.Patch
}

type Progress struct {
	Done, Total int64
	Delta       bool
	Applying    bool
}

type Prepared struct {
	owner   *Client
	options update.InstallOptions
	staging string
}

func (prepared *Prepared) Close() {
	if prepared != nil && prepared.staging != "" {
		_ = os.RemoveAll(prepared.staging)
		prepared.staging = ""
		prepared.options.Payload = ""
	}
}

func New(config Config) (*Client, error) {
	if config.PublicKey == "" || config.FeedURL == "" {
		return nil, ErrNotConfigured
	}
	key, err := update.ParsePublicKey(config.PublicKey)
	if err != nil {
		return nil, err
	}
	client, err := update.New(update.Config{
		Current: config.Version, App: buildinfo.AppID, Channel: config.Channel,
		FeedURL: config.FeedURL, PublicKeys: []ed25519.PublicKey{key}, HTTP: config.HTTP,
	})
	if err != nil {
		return nil, err
	}
	if config.Directory == "" {
		return nil, errors.New("missing update directory")
	}
	directory, err := filepath.Abs(config.Directory)
	if err != nil {
		return nil, err
	}
	return &Client{client: client, version: config.Version, directory: directory, install: update.Install}, nil
}

func (client *Client) Check(ctx context.Context) (*Offer, error) {
	feed, err := client.client.Check(ctx)
	if err != nil || feed == nil {
		return nil, err
	}
	full, err := feed.Asset(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS == "windows" && full.Format != update.FormatNSIS {
		return nil, fmt.Errorf("%w: missing Windows installer", update.ErrNoAsset)
	}
	offer := &Offer{owner: client, version: feed.Latest.Version, Version: feed.Latest.Version, FullSize: full.Size, full: full}
	for _, asset := range feed.Latest.Assets {
		if asset.OS != full.OS || asset.Arch != full.Arch {
			continue
		}
		if patch, ok := asset.PatchFrom(client.version); ok && patch.Size < full.Size && (offer.PatchSize == 0 || patch.Size < offer.PatchSize) {
			offer.patch, offer.patchAsset, offer.PatchSize = patch, asset, patch.Size
		}
	}
	return offer, nil
}

func (client *Client) Prepare(ctx context.Context, offer *Offer, progress func(Progress)) (*Prepared, error) {
	if offer == nil || offer.owner != client {
		return nil, errors.New("no verified update selected")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := privateDirectory(filepath.Dir(client.directory)); err != nil {
		return nil, err
	}
	if err := privateDirectory(client.directory); err != nil {
		return nil, err
	}
	cache := filepath.Join(client.directory, offer.full.SHA256)
	if err := privateDirectory(cache); err != nil {
		return nil, err
	}
	report := func(value Progress) {
		if progress != nil {
			progress(value)
		}
	}
	name := "markstack"
	switch runtime.GOOS {
	case "darwin":
		name = buildinfo.AppID
	case "windows":
		name = "markstack.exe"
	}
	options := update.InstallOptions{Asset: offer.full, Version: offer.version, App: name, Args: os.Args[1:]}
	if offer.patch.FromVersion != "" {
		patch, err := client.client.DownloadPatch(ctx, offer.patchAsset, offer.patch, cache, func(done, total int64) {
			report(Progress{Done: done, Total: total, Delta: true})
		})
		if err == nil && ctx.Err() == nil {
			report(Progress{Done: offer.patch.Size, Total: offer.patch.Size, Delta: true, Applying: true})
			payload, staging, err := client.applyPatch(ctx, patch, offer.version, cache)
			if err == nil {
				options.Payload, options.Asset = payload, offer.patchAsset
				return &Prepared{owner: client, options: options, staging: staging}, nil
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	artifact, err := client.client.Download(ctx, offer.full, cache, func(done, total int64) {
		report(Progress{Done: done, Total: total})
	})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	options.Artifact = artifact
	return &Prepared{owner: client, options: options}, nil
}

func (client *Client) Install(prepared *Prepared, quitFunc func()) error {
	if prepared == nil || prepared.owner != client {
		return errors.New("no prepared update to install")
	}
	return client.install(prepared.options, quitFunc)
}

func privateDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("unsafe update directory: %s", path)
	}
	return nil
}
