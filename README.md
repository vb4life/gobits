This repository holds my golang implementation of the [BITS Upload Protocol](https://msdn.microsoft.com/en-us/library/aa362828(v=vs.85).aspx).

## How to get
```
go get gitlab.com/magan/go-bits
```

## Example
Start the server with:
```
go-bits -listen=:8080 -uri=/ -root=./
```

After that, test an upload from a windows machine with the following PowerShell command:
```powershell
Start-BitsTransfer -TransferType Upload -Source <path to file to upload> -Destination http://<hostname>:<port>/<filename>
```

So, assuming that you have started go-bits on the local machine, and that yopu have a file called test.bin in the current PowerShell directory, the command would look like this:
```powershell
Start-BitsTransfer -TransferType Upload -Source test.bin -Destination http://localhost:8080/test.bin
```

*You can also have all the configuration in a file. Start go-bits with the following command instead:*
```
go-bits config=filename
```

The file will have one or more of the following settings:
```
[Settings]
Listen=<endpoint including port>
URI=<URI to filter requests>
Root=<directory>
TmpDir=<directory>
Protocol=<identifier of protocol>
```

## Settings
These are the settings that can be given either on the commandline or in the config file:

|Parameter|Description|
|--------|---|
|Listen  |Listen has the form of ip:port, for example 0.0.0.0:8080 to listen to all local IP addresses on port 8080. You can also limit it to a specific IP by giving it there. Exmple: 127.0.0.1:8080 will only listen to requests from localhost.|
|URI     |URI is the last part of the network path, excluding the filename. For example, if you upload a file to http://127.0.0.1:8080/test/file.bin, then the URI should be "/test/". If the beginning of the URL doesn't match the URI, it will be ignored. If the path has more folders, they will be stripped away. For example, http://127.0.0.1:8080/test/folder/file.bin will not create a subfolder called "folder".|
|Root    |Root is the path to the directory where the completed files will be placed.|
|TmpDir  |TmpDir is the path to where the uploads that are in progress are placed. By default, they will be plased in the temp directory, under a subdir called *GoBITS*.|
|Protocol|Protocol is the GUID of the protocol that should be used. There is only one [official](https://msdn.microsoft.com/en-us/library/aa362833(v=vs.85).aspx) GUID, but you can in theory create your own client/server with a different one.|

## Debug settings
If you want more (or less) information about what is happening, you can use one of the following switches at startup:

    -q  Only show errors
    -qq Don't show anything, ever!
    -v  Show additional information
    -vv Show trace information