// transferqueue is a finite Herdr plugin command for the local delivery store.
// Network credentials and task registration are deliberately not CLI arguments.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/felinics/memoh/internal/transferclient"
)

func main() { os.Exit(run()) }

func run() int {
	kind := flag.String("action", "probe", "probe, inspect, cancel, or retire")
	id := flag.String("delivery", "", "logical delivery ID")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected arguments")
		return 2
	}
	switch *kind {
	case "probe", "inspect", "cancel", "retire":
	default:
		fmt.Fprintln(os.Stderr, "unsupported local action")
		return 2
	}
	// This immutable package profile is recorded in the existing queue locator.
	// Changing it requires an explicit migration, never a fallback state root.
	limits := transferclient.QueueLimits{Jobs: 8, Bytes: 256 << 20, JournalBytes: 8 << 20, Workers: 1}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if *kind == "probe" {
		q, err := transferclient.OpenHerdrQueue(os.Getenv, "native-profile-v1", limits)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := q.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "queue close failed")
			return 1
		}
		fmt.Println(`{"storage":"available","transfer_binding":"required","profile":"native-profile-v1"}`)
		return 0
	}
	status, err := transferclient.InvokeHerdr(ctx, os.Getenv, "native-profile-v1", limits, transferclient.HerdrAction{Kind: *kind, DeliveryID: *id}, nil, nil, nil, time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := json.NewEncoder(os.Stdout).Encode(status); err != nil {
		return 1
	}
	return 0
}
