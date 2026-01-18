package main

import (
	"archive/zip"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
)

// XML structure matching the Danish vehicle registration format
type ESStatistikListeModtag struct {
	XMLName          xml.Name         `xml:"ESStatistikListeModtag_I"`
	StatistikSamling StatistikSamling `xml:"StatistikSamling"`
}

type StatistikSamling struct {
	Statistik []Statistik `xml:"Statistik"`
}

type Statistik struct {
	RegistreringNummerNummer        string                          `xml:"RegistreringNummerNummer"`
	KoeretoejOplysningGrundStruktur KoeretoejOplysningGrundStruktur `xml:"KoeretoejOplysningGrundStruktur"`
}

type KoeretoejOplysningGrundStruktur struct {
	KoeretoejBetegnelseStruktur KoeretoejBetegnelseStruktur `xml:"KoeretoejBetegnelseStruktur"`
}

type KoeretoejBetegnelseStruktur struct {
	KoeretoejMaerkeTypeNavn string `xml:"KoeretoejMaerkeTypeNavn"`
	Model                   Model  `xml:"Model"`
}

type Model struct {
	KoeretoejModelTypeNavn string `xml:"KoeretoejModelTypeNavn"`
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
	fileInput := flag.String("file", "", "Path to local XML or ZIP file (if not provided, downloads from FTP)")
	flag.Parse()

	// Use simple map instead of memdb
	plates := make(map[string]string, 100000) // Pre-allocate with estimated capacity

	if *fileInput != "" {
		log.Printf("Using local file: %s\n", *fileInput)
		if err := processLocalFile(*fileInput, plates); err != nil {
			log.Fatalf("Error processing local file: %v", err)
		}
	} else {
		log.Println("No file specified, downloading from FTP server...")
		if err := downloadAndProcess(plates); err != nil {
			log.Fatalf("Error downloading and processing: %v", err)
		}
	}

	displayResults(plates)
}

func processLocalFile(filePath string, plates map[string]string) error {
	ext := strings.ToLower(filePath[len(filePath)-4:])

	switch ext {
	case ".xml":
		file, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("failed to open XML file: %w", err)
		}
		defer file.Close()

		count, err := streamXML(file, plates)
		if err != nil {
			return err
		}

		fmt.Printf("\n✓ Successfully processed %d license plates\n", count)
		return nil

	case ".zip":
		return processZipFile(filePath, plates)

	default:
		return fmt.Errorf("unsupported file type: %s (must be .xml or .zip)", ext)
	}
}

func processZipFile(zipPath string, plates map[string]string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip file: %w", err)
	}
	defer r.Close()

	processedCount := 0

	for _, zipFile := range r.File {
		if zipFile.FileInfo().IsDir() || !strings.HasSuffix(strings.ToLower(zipFile.Name), ".xml") {
			continue
		}

		fmt.Printf("Processing: %s (%.2f MB)\n", zipFile.Name, float64(zipFile.UncompressedSize64)/(1024*1024))

		rc, err := zipFile.Open()
		if err != nil {
			log.Printf("Warning: failed to open %s: %v", zipFile.Name, err)
			continue
		}

		count, err := streamXML(rc, plates)
		rc.Close()

		if err != nil {
			log.Printf("Warning: failed to process %s: %v", zipFile.Name, err)
			continue
		}

		processedCount += count
	}

	fmt.Printf("\n✓ Successfully processed %d license plates\n", processedCount)
	return nil
}

func streamXML(reader io.Reader, plates map[string]string) (int, error) {
	decoder := xml.NewDecoder(reader)
	processedCount := 0

	// Reuse buffer for string concatenation
	var sb strings.Builder
	sb.Grow(64) // Pre-allocate reasonable size for make+model

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return processedCount, fmt.Errorf("XML parse error: %w", err)
		}

		if se, ok := token.(xml.StartElement); ok && se.Name.Local == "Statistik" {
			var stat Statistik

			if err := decoder.DecodeElement(&stat, &se); err != nil {
				log.Printf("Warning: failed to decode Statistik: %v", err)
				continue
			}

			if stat.RegistreringNummerNummer != "" {
				// Build concatenated make and model
				sb.Reset()
				sb.WriteString(stat.KoeretoejOplysningGrundStruktur.KoeretoejBetegnelseStruktur.KoeretoejMaerkeTypeNavn)
				sb.WriteString(" ")
				sb.WriteString(stat.KoeretoejOplysningGrundStruktur.KoeretoejBetegnelseStruktur.Model.KoeretoejModelTypeNavn)

				plates[stat.RegistreringNummerNummer] = sb.String()
				processedCount++

				if processedCount%10000 == 0 {
					fmt.Printf("  Processed %d plates...\n", processedCount)
				}
			}
		}
	}

	return processedCount, nil
}

func downloadAndProcess(plates map[string]string) error {
	conn, err := ftp.Dial("5.44.137.84:21", ftp.DialWithTimeout(10*time.Second))
	if err != nil {
		return fmt.Errorf("failed to connect to FTP: %w", err)
	}
	defer conn.Quit()

	if err = conn.Login("anonymous", "anonymous"); err != nil {
		return fmt.Errorf("failed to login: %w", err)
	}

	if err = conn.ChangeDir("/ESStatistikListeModtag"); err != nil {
		return fmt.Errorf("failed to change directory: %w", err)
	}

	entries, err := conn.List(".")
	if err != nil {
		return fmt.Errorf("failed to list directory: %w", err)
	}

	var newestZip *ftp.Entry
	for _, entry := range entries {
		if entry.Type == ftp.EntryTypeFile && strings.HasSuffix(entry.Name, ".zip") {
			if newestZip == nil || entry.Time.After(newestZip.Time) {
				newestZip = entry
			}
		}
	}

	if newestZip == nil {
		return fmt.Errorf("no zip files found in directory")
	}

	fmt.Printf("Downloading: %s (%s)\n", newestZip.Name, newestZip.Time.Format(time.RFC3339))
	fmt.Printf("File size: %.2f MB\n", float64(newestZip.Size)/(1024*1024))

	resp, err := conn.Retr(newestZip.Name)
	if err != nil {
		return fmt.Errorf("failed to retrieve file: %w", err)
	}
	defer resp.Close()

	tempFile, err := os.CreateTemp("", "ftp-zip-*.zip")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	progressReader := &ProgressReader{
		reader: resp,
		total:  int64(newestZip.Size),
	}

	written, err := io.Copy(tempFile, progressReader)
	if err != nil {
		return fmt.Errorf("failed to stream file: %w", err)
	}

	fmt.Printf("\n✓ Downloaded %d bytes\n", written)
	tempFile.Close()

	return processZipFile(tempFile.Name(), plates)
}

func displayResults(plates map[string]string) {
	// Convert map to sorted slice for display
	type plateEntry struct {
		plate         string
		makeModelName string
	}

	entries := make([]plateEntry, 0, len(plates))
	for plate, makeModel := range plates {
		entries = append(entries, plateEntry{plate, makeModel})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].plate < entries[j].plate
	})

	fmt.Printf("\n=== License Plates in Database (%d total) ===\n", len(entries))
	displayLimit := 10
	if len(entries) < displayLimit {
		displayLimit = len(entries)
	}

	for i := 0; i < displayLimit; i++ {
		fmt.Printf("%d. %s - %s\n", i+1, entries[i].plate, entries[i].makeModelName)
	}

	if len(entries) > displayLimit {
		fmt.Printf("... and %d more\n", len(entries)-displayLimit)
	}
}
