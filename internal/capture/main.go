// Command capture writes the testdata of go-qbittorrent. Run it with
// `go run . <subcommand>` from this folder.
//
//	webapi  Read the actions of each release from the qBittorrent source, and
//	        write testdata/webapi/. Set QBT_SRC to the path of a local
//	        qBittorrent clone, or the command clones the source itself.
package main

import (
	"fmt"
	"log"
	"os"
)

func main() {
	if len(os.Args) != 2 || os.Args[1] != "webapi" {
		fmt.Fprintln(os.Stderr, "usage: go run . webapi")
		os.Exit(2)
	}
	if err := captureWebAPI(); err != nil {
		log.Fatal(err)
	}
}
