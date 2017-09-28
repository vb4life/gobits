/*
GoBITS - A server implementation of Microsoft BITS (Background Intelligent Transfer Service) written in go.
Copyright (C) 2015  Magnus Andersson
*/

package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path"

	"log"

	"gitlab.com/magan/gobits"
)

func main() {

	// Default settings, not neccessary to change then, really
	cfg := &gobits.Config{
		TempDir:       path.Join(os.TempDir(), "gobits"),
		AllowedMethod: "BITS_POST",
		Protocol:      "{7df0354d-249b-430f-820d-3d2a9bef4931}",

		MaxSize: 200 * 1024 * 1024,

		Allowed: []string{
			".*",
		},

		Disallowed: []string{
			".*\\.exe",
			".*\\.msi",
		},
	}

	// Callback to handle events
	cb := func(event gobits.Event, session, path string) {
		switch event {
		case gobits.EventCreateSession:
			// This is just for informational purposes, not much we can do here..
			log.Printf("New session created: %v\n", session)

		case gobits.EventRecieveFile:
			// This is interesting. A file has been successfully been uploaded, and we must process it (move it or whatever)
			log.Printf("New file created: %v\n", path)
			os.Remove(path) // For debug purposes, just remove it

		case gobits.EventCloseSession:
			// A session is closed, meaning that all files in the session is completed. If you manage files in the EventRecievedFile above,
			// you only need to clean up the directory..
			log.Printf("Session closed: %v\n", session)
			os.RemoveAll(path)

		case gobits.EventCancelSession:
			// A session is canceled. Just cleanup the folder. If you have handled the BITS_EVENT_FILE
			log.Printf("Session canceled: %v\n", session)
			os.RemoveAll(path)

		}
	}

	bits, err := gobits.NewHandler(*cfg, cb)
	if err != nil {
		log.Fatalf("failed to create handler: %v", err)
	}

	http.Handle("/BITS/", bits)
	fmt.Println(http.ListenAndServe(":8080", nil))
}

func moveFile(src, dst string) (err error) {
	var fs os.FileInfo
	if fs, err = os.Stat(src); err != nil {
		return err
	}
	if !fs.Mode().IsRegular() {
		return fmt.Errorf("source must be a file")
	}

	var fd os.FileInfo
	if fd, err = os.Stat(dst); err != nil {
		if !os.IsNotExist(err) {
			// Some error with Stat
			return err
		}
		// File doesnt exist
	} else {
		// File exists
		if !fd.Mode().IsRegular() {
			return fmt.Errorf("destination must be a file")
		}
		if os.SameFile(fs, fd) {
			// No need to move the file, they are the same
			return nil
		}
	}

	// Best solution: Create a hard link and remove the old file
	if err = os.Link(src, dst); err != nil {
		// Ok, try and rename the file (move it)
		if err = os.Rename(src, dst); err != nil {
			// Failed to move it, then copy it
			if err = copyFileContents(src, dst); err != nil {
				// Well, what else can we do!?
				return err
			}
		}
	}
	err = os.Remove(src)
	return err
}

// copyFileContents copies the contents of the file named src to the file named
// by dst. The file will be created if it does not already exist. If the
// destination file exists, all it's contents will be replaced by the contents
// of the source file.
func copyFileContents(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return
	}
	err = out.Sync()
	return
}
