/*
GoBITS - A server implementation of Microsoft BITS (Background Intelligent Transfer Service) written in go.
Copyright (C) 2017  Magnus Andersson
*/

package gobits

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
)

func ExampleHandler() {

	// create config with defaults
	cfg := &Config{}

	// create callback function that just logs the events
	cb := func(event Event, session, path string) {
		log.Printf("got event: %v", event)
	}

	// create handler
	bits, err := NewHandler(*cfg, cb)
	if err != nil {
		log.Fatalf("failed to create handler: %v", err)
	}

	// setup handler and start serving
	http.Handle("/BITS/", bits)
	fmt.Println(http.ListenAndServe(":8080", nil))

}

func ExampleConfig_quick() {

	// this will create a simple config with sane defaults
	_ = &Config{}

}

func ExampleConfig_defaults() {

	_ = &Config{
		TempDir:       path.Join(os.TempDir(), "gobits"),
		AllowedMethod: "BITS_POST",
		Protocol:      "{7df0354d-249b-430f-820d-3d2a9bef4931}",
		MaxSize:       0, // <= 0 means no limit
		Allowed: []string{
			".*",
		},
		Disallowed: []string{},
	}

}

func ExampleCallbackFunc() {

	_ = func(event Event, session, path string) {
		switch event {
		case EventCreateSession:
			// This is just for informational purposes, not much we can do here..
			log.Printf("New session created: %v\n", session)

		case EventRecieveFile:
			// This is interesting. A file has been successfully been uploaded, and we must process it (move it or whatever)
			log.Printf("New file created: %v\n", path)
			os.Remove(path) // For debug purposes, just remove it

		case EventCloseSession:
			// A session is closed, meaning that all files in the session is completed. If you manage files in the EventRecievedFile above,
			// you only need to clean up the directory..
			log.Printf("Session closed: %v\n", session)
			os.RemoveAll(path)

		case EventCancelSession:
			// A session is canceled. Just cleanup the folder. If you have handled the EventRecieveFile
			log.Printf("Session canceled: %v\n", session)
			os.RemoveAll(path)

		}
	}

}
