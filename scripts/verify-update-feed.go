//go:build ignore

package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"log"
	"os"
	"strings"

	"github.com/vivaui/viva/update"
)

func main() {
	if len(os.Args) != 3 {
		log.Fatal("usage: verify-update-feed latest.json latest.json.sig")
	}
	key, err := update.ParsePublicKey(os.Getenv("MARKSTACK_UPDATE_PUBLIC_KEY"))
	if err != nil {
		log.Fatal("invalid release public key")
	}
	feed, err := os.ReadFile(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	signature, err := os.ReadFile(os.Args[2])
	if err != nil {
		log.Fatal(err)
	}
	signed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(signature)))
	if err != nil || !ed25519.Verify(key, feed, signed) {
		log.Fatal("the update feed does not verify against the public key embedded in release builds")
	}
}
