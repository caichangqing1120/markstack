package buildinfo

import "runtime/debug"

const AppID = "dev.markstack.app"

var (
	Version   = "0.1.0"
	Commit    = "development"
	Channel   = "development"
	FeedURL   = ""
	PublicKey = ""
)

func VivaVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dependency := range info.Deps {
			if dependency.Path == "github.com/vivaui/viva" {
				if dependency.Replace != nil {
					dependency = dependency.Replace
				}
				return dependency.Version
			}
		}
	}
	return "unknown"
}
