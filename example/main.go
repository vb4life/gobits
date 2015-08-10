/*
GoBITS - A server implementation of Microsoft BITS (Background Intelligent Transfer Service) written in go.
Copyright (C) 2015  Magnus Andersson
*/

package main

import (
	"path"
	"fmt"
	"gitlab.com/magan/gobits/bitsrv"
	"io"
	"net/http"
	"os"
)

func main() {

	// Default settings, not neccessary to change then, really
	cfg := &bitsrv.Config{
		TempDir:       path.Join(os.TempDir(), "gobits"),
		AllowedMethod: "BITS_POST",
		Protocol:      "{7df0354d-249b-430f-820d-3d2a9bef4931}",
	}

	// Callback to handle events
	cb := func(event bitsrv.BITSEvent, Session, Path string) {
		switch event {
		case bitsrv.EventCreateSession:
			// This is just for informational purposes, not much we can do here..
			fmt.Printf("New session created: %v\n", Session)

		case bitsrv.EventRecieveFile:
			// This is interesting. A file has been successfully been uploaded, and we must process it (move it or whatever)
			fmt.Printf("New file created: %v\n", Path)
			os.Remove(Path) // For debug purposes, just remove it

		case bitsrv.EventCloseSession:
			// A session is closed, meaning that all files in the session is completed. If you manage files in the EventRecievedFile above,
			// you only need to clean up the directory..
			fmt.Printf("Session closed: %v\n", Session)
			os.RemoveAll(Path)

		case bitsrv.EventCancelSession:
			// A session is canceled. Just cleanup the folder. If you have handled the BITS_EVENT_FILE
			fmt.Printf("Session canceled: %v\n", Session)
			os.RemoveAll(Path)
		
		}
	}

	bits := bitsrv.NewHandler(*cfg, cb)

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
			return fmt.Errorf("destination must be a file", dst)
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
