# autoplate

Current status:

download the newest zipped xml file from the public ftp server

streams the downloaded file, unzipping it and parsing the xml file in one step.

At present time the parsing is using wrong tags, so nothing is stored into the used memdb database.

Note: the download from the ftpserver takes a long time, this could probably be optimized by using more connections if possible.

## build autoplate

go build autoplate.go

## run autoplate

./autoplate

