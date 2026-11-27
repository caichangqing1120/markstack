package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"github.com/vivaui/viva/update"
	"markstack/internal/buildinfo"
	"markstack/internal/session"
	"markstack/internal/updater"
)

var (
	menuCheckUpdatesID = controlTree.ID(page, "menu-check-updates")
	autoCheckUpdatesID = controlTree.ID(page, "auto-check-updates")
	checkUpdatesID     = controlTree.ID(page, "check-updates")
	downloadUpdateID   = controlTree.ID(page, "download-update")
	cancelUpdateID     = controlTree.ID(page, "cancel-update")
	installUpdateID    = controlTree.ID(page, "install-update")
	copyDiagnosticsID  = controlTree.ID(page, "copy-diagnostics")
	projectRepoID      = controlTree.ID(page, "project-repository")
	projectIssuesID    = controlTree.ID(page, "project-issues")
)

const projectURL = "https://github.com/caichangqing1120/markstack"

type updateService interface {
	Check(context.Context) (*updater.Offer, error)
	Prepare(context.Context, *updater.Offer, func(updater.Progress)) (*updater.Prepared, error)
	Install(*updater.Prepared, func()) error
}

type updatePhase uint8

const (
	updateIdle updatePhase = iota
	updateChecking
	updateCurrent
	updateAvailable
	updateDownloading
	updateReady
	updateCanceled
	updateFailed
)

type updatePanel struct {
	service     updateService
	quit        func()
	phase       updatePhase
	err         error
	offer       *updater.Offer
	prepared    *updater.Prepared
	progress    updater.Progress
	cancel      context.CancelFunc
	download    *updateDownload
	quitPending bool
	installing  bool
	copied      bool
}

type updateDownload struct {
	done     chan struct{}
	prepared *updater.Prepared
}

func (panel *updatePanel) busy() bool {
	return panel.cancel != nil || panel.installing || panel.quitPending
}

func (panel *updatePanel) close() {
	if panel.cancel != nil {
		panel.cancel()
	}
	if panel.download != nil {
		<-panel.download.done
		panel.download.prepared.Close()
	}
	if !panel.installing {
		panel.prepared.Close()
	}
}

func (state *model) startUpdateCheckOnLaunch() {
	if state.preferences.AutoCheckUpdates {
		state.startUpdateCheck()
	}
}

func (state *model) configureUpdates() {
	store, err := defaultPreferencesStore()
	if err == nil {
		state.updates.service, err = updater.New(updater.Config{
			Version: buildinfo.Version, Channel: buildinfo.Channel, FeedURL: buildinfo.FeedURL,
			PublicKey: buildinfo.PublicKey, Directory: filepath.Join(filepath.Dir(store.Path), "updates"),
		})
	}
	if err != nil {
		state.updates.service = nil
		state.updates.err = err
	}
}

func (state *model) startUpdateCheck() {
	panel := &state.updates
	if panel.busy() || panel.service == nil {
		return
	}
	panel.prepared.Close()
	panel.prepared, panel.offer, panel.err = nil, nil, nil
	panel.phase = updateChecking
	ctx, cancel := context.WithCancel(context.Background())
	panel.cancel = cancel
	service := panel.service
	go func() {
		offer, err := service.Check(ctx)
		state.invalidator.Post(func() {
			panel.cancel = nil
			switch {
			case ctx.Err() != nil:
				panel.phase = updateCanceled
			case err != nil:
				panel.phase, panel.err = updateFailed, err
			case offer == nil:
				panel.phase = updateCurrent
			default:
				panel.phase, panel.offer = updateAvailable, offer
			}
			cancel()
		})
	}()
}

func (state *model) startUpdateDownload() {
	panel := &state.updates
	if panel.busy() || panel.offer == nil || panel.prepared != nil {
		return
	}
	panel.phase, panel.err, panel.progress = updateDownloading, nil, updater.Progress{}
	ctx, cancel := context.WithCancel(context.Background())
	panel.cancel = cancel
	service, offer := panel.service, panel.offer
	download := &updateDownload{done: make(chan struct{})}
	panel.download = download
	go func() {
		defer close(download.done)
		var last time.Time
		var previous updater.Progress
		prepared, err := service.Prepare(ctx, offer, func(progress updater.Progress) {
			if progress.Delta != previous.Delta || progress.Applying || progress.Done == progress.Total || time.Since(last) >= 70*time.Millisecond {
				last, previous = time.Now(), progress
				state.invalidator.Post(func() { panel.progress = progress })
			}
		})
		download.prepared = prepared
		if !state.invalidator.Post(func() {
			panel.cancel, panel.download = nil, nil
			switch {
			case ctx.Err() != nil:
				prepared.Close()
				panel.phase = updateCanceled
			case err != nil:
				panel.phase, panel.err = updateFailed, err
			default:
				panel.phase, panel.prepared = updateReady, prepared
			}
			cancel()
		}) {
			prepared.Close()
			cancel()
		}
	}()
}

func (state *model) requestUpdateInstall() {
	panel := &state.updates
	if panel.busy() || panel.prepared == nil || panel.quit == nil || state.session.Pending() != nil {
		return
	}
	panel.quitPending = true
	state.requestClose(func() {
		panel.quitPending, panel.installing = false, true
		state.openSettingsSection(settingsAbout)
		service, prepared := panel.service, panel.prepared
		go func() {
			approved := false
			err := service.Install(prepared, func() { approved = true })
			state.invalidator.Post(func() {
				panel.installing = false
				if approved {
					panel.prepared = nil
					prepared.Close()
					state.finishQuitRecovery()
					panel.quit()
					return
				}
				state.quitDiscarded = false
				if err == nil {
					err = errors.New("installer did not confirm restart")
				}
				panel.phase, panel.err = updateFailed, err
			})
		}()
	})
}

func (state *model) updateStatusText() string {
	panel := &state.updates
	local := state.local
	switch {
	case panel.installing:
		return local("正在准备安装并重启…", "Preparing to install and restart…")
	case errors.Is(panel.err, update.ErrManagedExternally):
		return local("此版本由系统管理；请通过包管理器升级。", "Installed by your system. Update it with your package manager.")
	case errors.Is(panel.err, update.ErrNotWritable):
		return local("应用目录不可写；请手动安装新版。", "The app location is not writable. Install the new version manually.")
	case errors.Is(panel.err, update.ErrSignature):
		return local("更新签名无法验证，已拒绝。", "Couldn't verify the update signature. Update rejected.")
	case panel.service == nil && (panel.err == nil || errors.Is(panel.err, updater.ErrNotConfigured)):
		return local("此构建未配置自动升级。", "Updates are not configured for this build.")
	case panel.err != nil:
		return local("更新失败：", "Update failed: ") + panel.err.Error()
	}
	switch panel.phase {
	case updateChecking:
		return local("正在检查更新…", "Checking for updates…")
	case updateCurrent:
		return local("已是最新版本。", "You're up to date.")
	case updateAvailable:
		return local("发现新版本 ", "Update available: ") + panel.offer.Version
	case updateDownloading:
		if panel.progress.Applying {
			return local("正在应用增量补丁…", "Applying delta patch…")
		}
		if panel.progress.Delta {
			return local("正在下载增量补丁…", "Downloading delta patch…")
		}
		return local("正在下载全量包…", "Downloading full package…")
	case updateReady:
		return local("下载并校验完成，可以安装。", "Verified and ready to install.")
	case updateCanceled:
		return local("已取消更新", "Update canceled")
	default:
		return local("尚未检查更新。", "Not checked yet.")
	}
}

func (view settingsView) paintAbout(bounds ui.Rect) {
	state := view.state
	panel := &state.updates
	local := state.local
	card := bounds
	view.card(card, 136, local("构建信息", "Build information"))
	platform := runtime.GOOS + "/" + runtime.GOARCH
	view.aboutLabel(card, 29, local("版本 ", "Version ")+buildinfo.Version+" · "+platform+" · "+buildinfo.Channel, view.palette.ink)
	view.aboutLabel(card, 52, "Commit: "+buildinfo.Commit, view.palette.muted)
	view.aboutLabel(card, 75, runtime.Version()+" · Viva "+buildinfo.VivaVersion(), view.palette.muted)
	copyLabel := local("复制诊断信息", "Copy diagnostics")
	if panel.copied {
		copyLabel = local("已复制", "Copied")
	}
	view.button(copyDiagnosticsID, copyLabel, copyLabel, ui.Rect{X: card.X + 12, Y: card.Y + 103, Width: 155, Height: 25}, panel.copied, !panel.installing, state.copyDiagnostics)
	card.Y += 144
	view.card(card, 181, local("增量更新", "Software updates"))
	auto := local("启动时检查更新", "Check on startup")
	view.toggle(autoCheckUpdatesID, view.row(card, 31, auto), auto, state.preferences.AutoCheckUpdates, func(next *session.Preferences) { next.AutoCheckUpdates = !next.AutoCheckUpdates })
	view.aboutLabel(card, 57, state.updateStatusText(), view.palette.ink)
	if panel.offer != nil && (panel.progress.Total <= 0 || panel.phase != updateDownloading) {
		offer := panel.offer
		badge := local("全量包 ", "Full ") + updateSize(offer.FullSize)
		if offer.PatchSize > 0 {
			badge = local("增量补丁 ", "Delta ") + updateSize(offer.PatchSize) + " / " + badge
		}
		rect := ui.Rect{X: card.X + 12, Y: card.Y + 86, Width: min(card.Width-24, view.canvas.TextWidth(badge, state.uiSize(12))+18), Height: 23}
		view.canvas.Rounded(rect, 5, view.palette.sidebar)
		view.label(badge, rect.Inset(gfx.Insets{Left: 8, Right: 8}), 12, gfx.WeightRegular, view.palette.accent)
	}
	if panel.phase == updateDownloading {
		track := ui.Rect{X: card.X + 12, Y: card.Y + 119, Width: card.Width - 80, Height: 6}
		view.canvas.Rounded(track, 3, view.palette.sidebar)
		if panel.progress.Total > 0 {
			fraction := float32(panel.progress.Done) / float32(panel.progress.Total)
			track.Width *= min(float32(1), max(float32(0), fraction))
			view.canvas.Rounded(track, 3, view.palette.accent)
			view.label(fmt.Sprintf("%.0f%%", fraction*100), ui.Rect{X: card.X + card.Width - 58, Y: card.Y + 110, Width: 46, Height: 23}, 12, gfx.WeightMedium, view.palette.ink)
		}
	}
	check := local("立即检查更新", "Check for updates")
	checkRect := ui.Rect{X: card.X + 12, Y: card.Y + 142, Width: 152, Height: 28}
	view.button(checkUpdatesID, check, check, checkRect, false, !panel.busy() && panel.service != nil, state.startUpdateCheck)
	actionRect := ui.Rect{X: card.X + 173, Y: checkRect.Y, Width: 164, Height: 28}
	switch {
	case panel.cancel != nil:
		label := local("取消", "Cancel")
		view.button(cancelUpdateID, label, label, actionRect, false, true, panel.cancel)
	case panel.prepared != nil:
		label := local("安装并重启", "Install and restart")
		view.button(installUpdateID, label, label, actionRect, true, !panel.busy() && panel.quit != nil, state.requestUpdateInstall)
	case panel.offer != nil:
		label := local("下载更新", "Download update")
		view.button(downloadUpdateID, label, label, actionRect, true, !panel.busy(), state.startUpdateDownload)
	}
	card.Y += 189
	view.card(card, 70, local("项目链接", "Project links"))
	for index, link := range []struct {
		id         gfx.ID
		label, url string
	}{
		{projectRepoID, local("项目仓库", "Repository"), projectURL},
		{projectIssuesID, local("问题反馈", "Issues"), projectURL + "/issues"},
	} {
		view.button(link.id, link.label, link.label, ui.Rect{X: card.X + 12 + float32(index)*134, Y: card.Y + 32, Width: 124, Height: 27}, false, !panel.installing, func() {
			if err := app.OpenURL(link.url); err != nil {
				state.settings.feedback = local("无法打开链接：", "Couldn't open link: ") + err.Error()
			}
		})
	}
}

func (view settingsView) aboutLabel(card ui.Rect, offset float32, text string, color ui.Color) {
	view.label(text, ui.Rect{X: card.X + 12, Y: card.Y + offset, Width: card.Width - 24, Height: 22}, 12, gfx.WeightRegular, color)
}

func updateSize(bytes int64) string {
	if bytes >= 1<<20 {
		return fmt.Sprintf("%.1f MiB", float64(bytes)/(1<<20))
	}
	if bytes >= 1<<10 {
		return fmt.Sprintf("%.1f KiB", float64(bytes)/(1<<10))
	}
	return fmt.Sprintf("%d B", bytes)
}

func (state *model) copyDiagnostics() {
	value := strings.Join([]string{
		"MarkStack: " + buildinfo.Version, "Channel: " + buildinfo.Channel, "Commit: " + buildinfo.Commit,
		"Platform: " + runtime.GOOS + "/" + runtime.GOARCH, "Go: " + runtime.Version(), "Viva: " + buildinfo.VivaVersion(),
	}, "\n")
	if err := app.SetClipboardText(value); err != nil {
		state.settings.feedback = state.local("复制失败：", "Couldn't copy diagnostics: ") + err.Error()
		return
	}
	state.updates.copied = true
	state.settings.feedback = ""
}
