# autoplate

Current status:

download the newest zipped xml file from the public ftp server

streams the downloaded file, unzipping it and parsing the xml file in one step.

When finished, a short status with the first 10 plates are listed

Note: the download from the ftpserver takes a relatively long time, this could probably be optimized by using more connections if possible.

## build autoplate

go build autoplate.go

## run autoplate

./autoplate

If you allready have dowloaded the .zip file (or have the extracted .xml file) this can be used as input instead of the default downloading of the newest file.

./autoplate optionalZipOrXmlfile

