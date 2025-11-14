package main

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"time"

	"github.com/hashicorp/go-memdb"
	"github.com/jlaffaye/ftp"
)

// LicensePlate represents a license plate record
type LicensePlate struct {
	Plate     string
	Timestamp time.Time
}

// Vehicle represents the XML structure (adjust based on actual XML format)
type Vehicle struct {
	XMLName      xml.Name `xml:"Vehicle"`
	LicensePlate string   `xml:"LicensePlate"`
}

// ProgressReader wraps an io.Reader and reports progress
type ProgressReader struct {
	reader    io.Reader
	total     int64
	current   int64
	lastPrint int64
}

func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.current += int64(n)

	// Print progress every 1% or 1MB
	if pr.total > 0 {
		percentDone := (pr.current * 100) / pr.total
		if percentDone > pr.lastPrint {
			pr.lastPrint = percentDone
			fmt.Printf("\rDownloading: %d%% (%d / %d bytes)", percentDone, pr.current, pr.total)
		}
	}

	return n, err
}

func main() {
	// Connect to FTP server
	conn, err := ftp.Dial("5.44.137.84:21", ftp.DialWithTimeout(10*time.Second))
	if err != nil {
		log.Fatalf("Failed to connect to FTP: %v", err)
	}
	defer conn.Quit()

	// Login (adjust credentials as needed)
	err = conn.Login("anonymous", "anonymous")
	if err != nil {
		log.Fatalf("Failed to login: %v", err)
	}

	// Change to target directory
	err = conn.ChangeDir("/ESStatistikListeModtag")
	if err != nil {
		log.Fatalf("Failed to change directory: %v", err)
	}

	// Find newest zip file
	entries, err := conn.List(".")
	if err != nil {
		log.Fatalf("Failed to list directory: %v", err)
	}

	var newestZip *ftp.Entry
	for _, entry := range entries {
		if entry.Type == ftp.EntryTypeFile && len(entry.Name) > 4 && entry.Name[len(entry.Name)-4:] == ".zip" {
			if newestZip == nil || entry.Time.After(newestZip.Time) {
				newestZip = entry
			}
		}
	}

	if newestZip == nil {
		log.Fatal("No zip files found in directory")
	}

	fmt.Printf("Downloading: %s (%s)\n", newestZip.Name, newestZip.Time.Format(time.RFC3339))
	fmt.Printf("File size: %.2f MB\n", float64(newestZip.Size)/(1024*1024))

	// Download zip file to temporary file for streaming
	resp, err := conn.Retr(newestZip.Name)
	if err != nil {
		log.Fatalf("Failed to retrieve file: %v", err)
	}
	defer resp.Close()

	// Create temporary file for streaming
	tempFile, err := os.CreateTemp("", "ftp-zip-*.zip")
	if err != nil {
		log.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	// Initialize memdb
	db, err := setupMemDB()
	if err != nil {
		log.Fatalf("Failed to setup memdb: %v", err)
	}

	// Create progress reader
	progressReader := &ProgressReader{
		reader: resp,
		total:  int64(newestZip.Size),
	}

	// Stream download to temp file with progress
	written, err := io.Copy(tempFile, progressReader)
	if err != nil {
		log.Fatalf("Failed to stream file: %v", err)
	}

	fmt.Printf("\n✓ Downloaded %d bytes\n", written)

	// Reset file pointer to beginning
	_, err = tempFile.Seek(0, 0)
	if err != nil {
		log.Fatalf("Failed to seek temp file: %v", err)
	}

	// Process zip file by streaming each entry
	err = processZipStream(tempFile, written, db)
	if err != nil {
		log.Fatalf("Failed to process zip: %v", err)
	}

	// Display results
	displayResults(db)
}

func setupMemDB() (*memdb.MemDB, error) {
	schema := &memdb.DBSchema{
		Tables: map[string]*memdb.TableSchema{
			"plates": {
				Name: "plates",
				Indexes: map[string]*memdb.IndexSchema{
					"id": {
						Name:    "id",
						Unique:  true,
						Indexer: &memdb.StringFieldIndex{Field: "Plate"},
					},
				},
			},
		},
	}

	return memdb.NewMemDB(schema)
}

func processZipStream(file *os.File, size int64, db *memdb.MemDB) error {
	reader, err := zip.NewReader(file, size)
	if err != nil {
		return fmt.Errorf("failed to create zip reader: %w", err)
	}

	txn := db.Txn(true)
	defer txn.Abort()

	processedCount := 0

	for _, zipFile := range reader.File {
		if zipFile.FileInfo().IsDir() {
			continue
		}

		fmt.Printf("Processing: %s (%.2f KB)\n", zipFile.Name, float64(zipFile.UncompressedSize64)/1024)

		// Open file in zip for streaming
		rc, err := zipFile.Open()
		if err != nil {
			log.Printf("Warning: failed to open %s: %v", zipFile.Name, err)
			continue
		}

		// Stream parse XML without loading entire file
		decoder := xml.NewDecoder(rc)
		
		for {
			token, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				log.Printf("Warning: XML parse error in %s: %v", zipFile.Name, err)
				break
			}

			// Look for Vehicle start elements
			if se, ok := token.(xml.StartElement); ok {
				if se.Name.Local == "Vehicle" {
					var vehicle Vehicle
					if err := decoder.DecodeElement(&vehicle, &se); err != nil {
						log.Printf("Warning: failed to decode vehicle: %v", err)
						continue
					}

					if vehicle.LicensePlate != "" {
						plate := &LicensePlate{
							Plate:     vehicle.LicensePlate,
							Timestamp: time.Now(),
						}
						if err := txn.Insert("plates", plate); err != nil {
							log.Printf("Warning: failed to insert plate: %v", err)
						}
						processedCount++
						
						// Progress indicator
						if processedCount%1000 == 0 {
							fmt.Printf("  Processed %d plates...\n", processedCount)
						}
					}
				}
			}
		}

		rc.Close()
	}

	txn.Commit()
	fmt.Printf("\n✓ Successfully processed %d license plates\n", processedCount)
	return nil
}

func displayResults(db *memdb.MemDB) {
	txn := db.Txn(false)
	defer txn.Abort()

	it, err := txn.Get("plates", "id")
	if err != nil {
		log.Printf("Failed to query plates: %v", err)
		return
	}

	var plates []string
	for obj := it.Next(); obj != nil; obj = it.Next() {
		p := obj.(*LicensePlate)
		plates = append(plates, p.Plate)
	}

	sort.Strings(plates)

	fmt.Printf("\n=== License Plates in Database (%d total) ===\n", len(plates))
	for i, plate := range plates {
		fmt.Printf("%d. %s\n", i+1, plate)
		if i >= 9 {
			fmt.Printf("... and %d more\n", len(plates)-10)
			break
		}
	}
}