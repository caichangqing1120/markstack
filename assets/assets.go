package assets

import (
	_ "embed"
	"sync"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/app/iconpng"
)

//go:embed markstack-icon.png
var iconPNG []byte

var WindowIcon = sync.OnceValue(func() app.WindowIcon {
	icon, err := iconpng.DecodeFit(iconPNG, 512)
	if err != nil {
		panic(err)
	}
	return icon
})
