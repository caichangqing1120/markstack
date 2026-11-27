package updater

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/update"
	"markstack/internal/buildinfo"
)

const patchHelperFlag = "--markstack-apply-update"

func RunPatchHelper(args []string) (bool, error) {
	if len(args) == 0 || args[0] != patchHelperFlag {
		return false, nil
	}
	if len(args) != 4 {
		return true, errors.New("invalid update helper arguments")
	}
	_, err := update.ApplyPatch(args[1], buildinfo.AppID, args[2], args[3])
	return true, err
}

func (client *Client) applyPatch(ctx context.Context, patch, version, cache string) (payload, staging string, err error) {
	executable := client.executable
	if executable == "" {
		executable, err = os.Executable()
		if err != nil {
			return "", "", err
		}
	}
	stage, err := os.MkdirTemp(cache, "stage-")
	if err != nil {
		return "", "", err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(stage)
		}
	}()
	command := exec.CommandContext(ctx, executable, patchHelperFlag, patch, client.version, version)
	command.Env = append(os.Environ(), "TMPDIR="+stage, "TMP="+stage, "TEMP="+stage)
	if output, err := command.CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("apply update patch: %w: %s", err, output)
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	entries, err := os.ReadDir(stage)
	if err != nil {
		return "", "", err
	}
	if len(entries) != 1 || !entries[0].IsDir() || !strings.HasPrefix(entries[0].Name(), "viva-payload-") {
		return "", "", errors.New("update helper did not stage a payload")
	}
	return filepath.Join(stage, entries[0].Name()), stage, nil
}
