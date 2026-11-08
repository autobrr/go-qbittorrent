// Command capture writes the testdata of go-qbittorrent. Run it with
// `go run . <subcommand>` from this folder.
//
//	features                 Probe the features that qui gates by Web API version, and
//	                         write testdata/features/.
//	preferences [version...] Write the app/preferences response of each version, or
//	                         of the given versions, to testdata/preferences/.
//	torrents                 Write real torrent responses to testdata/torrents/.
//	webapi                   Read the actions of each release from the qBittorrent source, and
//	                         write testdata/webapi/. Set QBT_SRC to the path of a local
//	                         qBittorrent clone, or the command clones the source itself.
//
// features, preferences, and torrents run each release in a local Docker container,
// and need Docker and mkbrr.
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

const usage = "usage: go run . features | preferences [version...] | torrents | webapi"

func main() {
	if len(os.Args) < 2 || os.Args[1] != "preferences" && len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	scratch, err := os.MkdirTemp("", "qbt-capture-")
	if err != nil {
		log.Fatal(err)
	}
	cleanup := func() {
		removeContainer()
		os.RemoveAll(scratch)
	}
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-interrupt
		cleanup()
		os.Exit(130)
	}()

	switch os.Args[1] {
	case "features":
		err = captureFeatures(scratch)
	case "preferences":
		err = capturePreferences(os.Args[2:])
	case "torrents":
		err = captureTorrents(scratch)
	case "webapi":
		err = captureWebAPI()
	default:
		cleanup()
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	cleanup()
	if err != nil {
		log.Fatal(err)
	}
}
