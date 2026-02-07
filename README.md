# autoplate

Current status:

Downloads the newest zipped xml file from the public ftp server.

Streams the downloaded file, unzipping it and parsing the xml file in one step.

When finished, a short status with the first 10 plates are listed.

Note:

1) The download from the ftpserver takes a relatively long time, this could probably be optimized by using more connections if possible.

2) Make sure that you have about 7 gigabyte a free diskspace, since the downloaded zip file is huge. If you want to study the unzipped xml file, it is about 130 gigabyte.

3) The parsing and storing in memory can be done on a low end computer with 8 gigabyte memmory. It stores the plates in a map consisting of pairs of <"platename", "make + model">. The memory consumption could probably be optimized further.

## build autoplate

go build autoplate.go

## run autoplate

./autoplate

If you allready have downloaded the .zip file (or have the extracted .xml file) this can be used as input instead of the default downloading of the newest file.

./autoplate -file optionalZipOrXmlfile

if you using the supplied test example.

./autoplate -file ./test/ESStatistikListeModtag-20261102-165603.zip 


