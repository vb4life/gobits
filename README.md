# GoBITS

[![GoDoc](https://godoc.org/gitlab.com/magan/gobits?status.svg)](https://godoc.org/gitlab.com/magan/gobits)

This repository holds my golang implementation of the [BITS Upload Protocol](https://msdn.microsoft.com/en-us/library/aa362828(v=vs.85).aspx).

It has been tested on Windows 10 and Debian 8, so it _should_ work on all platforms.

## How to get
```
go install gitlab.com/magan/gobits
```
[More detail here](https://gitlab.com/magan/gobits/wikis/install)

## Configuration
[More detail here](https://gitlab.com/magan/gobits/wikis/configure)

## Examples
You can find an example implementation [here](https://gitlab.com/magan/gobits/tree/master/example)

In short, it implements the ServeHTTP handler, so you can use it on an existing webserver written in go by simply adding the following lines of code:
```golang
import gitlab.com/magan/gobits
```

```golang
cb := func(event gobits.Event, session, path string) {
	switch event {
	case gobits.EventCreateSession:
		fmt.Printf("new session created: %v\n", session)
	case gobits.EventRecieveFile:
		fmt.Printf("new file created: %v\n", path)
	case gobits.EventCloseSession:
		fmt.Printf("session closed: %v\n", session)
	case gobits.EventCancelSession:
		fmt.Printf("session canceled: %v\n", session)
	}
}
bits := gobits.NewHandler(gobits.Config{}, cb)
http.Handle("/BITS/", bits)
```

After that, test an upload from a windows machine with the following PowerShell command:
```powershell
Start-BitsTransfer -TransferType Upload -Source <path to file to upload> -Destination http://<hostname>:<port>/BITS/<filename>
```