module markstack

go 1.26.0

require (
	github.com/alecthomas/chroma/v2 v2.27.0
	github.com/go-pdf/fpdf v0.9.0
	github.com/vivaui/viva v0.6.1-0.20261112052740-64dd307fdc60
	github.com/yuin/goldmark v1.8.7
	golang.org/x/net v0.62.0
	golang.org/x/sys v0.49.0
)

require (
	github.com/dlclark/regexp2/v2 v2.2.1 // indirect
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/pelletier/go-toml/v2 v2.4.3 // indirect
)

tool github.com/vivaui/viva/cmd/viva

replace github.com/vivaui/viva => github.com/caichangqing1120/viva v0.6.1-0.20261112084946-75b4be987303
