// Command capture writes the testdata of go-qbittorrent. Run it with
// `go run . <subcommand>` from this folder.
//
//	features                 Probe the features that qui gates by Web API version, and
//	                         write testdata/features/.
//	filters [version...]     Record the torrents/info results of each filter, category, and
//	                         tag in each version, or in the given versions, and write
//	                         testdata/filters/.
//	preferences [version...] Write the app/preferences response of each version, or
//	                         of the given versions, to testdata/preferences/.
//	torrents                 Write real torrent responses to testdata/torrents/.
//	webapi                   Read the actions of each release from the qBittorrent source, and
//	                         write testdata/webapi/. Set QBT_SRC to the path of a local
//	                         qBittorrent clone, or the command clones the source itself.
//
// features, filters, preferences, and torrents run each release in a local Docker container,
// and need Docker and mkbrr.
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

const usage = "usage: go run . features | filters [version...] | preferences [version...] | torrents | webapi"

func main() {
	if len(os.Args) < 2 || os.Args[1] != "preferences" && os.Args[1] != "filters" && len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	// The scratch folder holds a few hundred MiB of seed data, so keep it out of a tmpfs.
	cache, err := os.UserCacheDir()
	if err != nil {
		log.Fatal(err)
	}
	scratch, err := os.MkdirTemp(cache, "qbt-capture-")
	if err != nil {
		log.Fatal(err)
	}
	cleanup := func() {
		runCleanups()
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
	case "filters":
		err = captureFilters(scratch, os.Args[2:])
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
