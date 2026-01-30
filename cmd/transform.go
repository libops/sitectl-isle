package cmd

import (
	"archive/zip"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"log/slog"

	"github.com/libops/sitectl-isle/pkg/islandora"
	"github.com/libops/sitectl-isle/pkg/proquest"
	"github.com/spf13/cobra"
	"golang.org/x/net/html/charset"
)

var target string
var source string

// transformCmd represents the transform command
var transformCmd = &cobra.Command{
	Use:   "transform",
	Short: "Transform one storage format to another",
}

func init() {
	transformCmd.AddCommand(transformEtdCmd)
	transformCmd.PersistentFlags().StringVar(&source, "source", "", "Source file")
	transformCmd.PersistentFlags().StringVar(&target, "target", "", "Where to save target file")
}

// transformEtdCmd represents the csvTransform command
var transformEtdCmd = &cobra.Command{
	Use:   "etd",
	Short: "Transform a directory of ProQuest ETD ZIPs to another format",
	Run:   transformETDs,
}

// transformETDs reads ZIP files from "etds" directory, extracts XML, and writes a CSV
func transformETDs(cmd *cobra.Command, args []string) {
	isDir, err := isDirectory(source)
	if !isDir || err != nil {
		slog.Error("Source flag is not a directory", "source", source)
		os.Exit(1)
	}

	if target == "" {
		slog.Error("Target flag is required")
		os.Exit(1)
	}

	outFile, err := os.Create(target)
	if err != nil {
		slog.Error("Failed to create output file", "error", err)
		return
	}
	defer outFile.Close()

	writer := csv.NewWriter(outFile)
	defer writer.Flush()

	header := []string{
		"Upload ID",
		"Page/Item Parent ID",
		"Child Sort Order",
		"Node ID",
		"Parent Collection",
		"Object Model",
		"File Path",
		"Add Coverpage (Y/N)",
		"Title",
		"Full Title",
		"Make Public (Y/N)",
		"Contributor",
		"Related Department",
		"Resource Type",
		"Genre (Getty AAT)",
		"Creation Date",
		"Embargo Until Date",
		"Language",
		"File Format (MIME Type)",
		"Digital Origin",
		"Abstract",
		"Subject Topic (LCSH)",
		"Keyword",
		"Rights Statement",
		"Supplemental File",
	}

	if err := writer.Write(header); err != nil {
		slog.Error("Failed to write CSV header", "error", err)
		return
	}

	// Iterate over ZIP files
	uploadId := 1
	err = filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			slog.Error("Error accessing file", "file", path, "error", err)
			return nil
		}
		if strings.HasSuffix(info.Name(), ".zip") {
			slog.Info("Processing ZIP file", "file", path)
			if err := processZip(uploadId, path, writer); err != nil {
				slog.Error("Failed to process ZIP", "file", path, "error", err)
			}
			uploadId += 1
		}
		return nil
	})

	if err != nil {
		slog.Error("Failed to walk directory", "error", err)
	}
}

// processZip extracts XML from a ZIP, finds the relevant data, and writes to CSV
func processZip(uploadId int, zipPath string, writer *csv.Writer) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer r.Close()

	var xmlFile *zip.File
	var pdfFile string
	var supplementaryFiles []string

	for _, file := range r.File {
		if strings.HasSuffix(file.Name, "_DATA.xml") {
			xmlFile = file
		} else if strings.HasSuffix(file.Name, ".pdf") {
			pdfFile = filepath.Join(source, file.Name)
		} else if strings.Contains(file.Name, "/") && !strings.HasSuffix(file.Name, "/") {
			supplementaryFiles = append(supplementaryFiles, filepath.Join(source, file.Name))
		}
	}

	if xmlFile == nil {
		return fmt.Errorf("no _DATA.xml file found in %s", zipPath)
	}

	xmlReader, err := xmlFile.Open()
	if err != nil {
		return fmt.Errorf("failed to open XML file in ZIP: %w", err)
	}
	defer xmlReader.Close()

	decoder := xml.NewDecoder(xmlReader)
	decoder.CharsetReader = charset.NewReaderLabel // Enable charset conversion

	var submission proquest.DISSSubmission
	if err := decoder.Decode(&submission); err != nil {
		return fmt.Errorf("failed to decode XML: %w", err)
	}

	// Format keywords
	var keywords []string
	for _, keyword := range submission.Description.Categorization.Keywords {
		keyword = strings.ReplaceAll(keyword, ", ", " ; ")
		keywords = append(keywords, keyword)
	}

	// Format subjects
	var subjects []string
	for _, category := range submission.Description.Categorization.Categories {
		subjects = append(subjects, category.Description)
	}

	year, err := time.Parse("2006-01", submission.Description.Dates.CompletionDate)
	if err != nil {
		slog.Error("Invalid completion year format", "date", submission.Description.Dates.CompletionDate, "error", err)
		return err
	}
	title := submission.Description.Title
	if len(title) > 255 {
		title = submission.Description.Title[0:255]
	}
	genre := "theses"
	if submission.Description.Degree == "Ph.D." {
		genre = "dissertations"
	}
	language := submission.Description.Categorization.Language
	if language == "en" {
		language = "English"
	} else {
		return fmt.Errorf("unknown language: %s", language)
	}
	row := []string{
		strconv.Itoa(uploadId),
		"",
		"",
		"",
		"202",
		"Digital Document",
		pdfFile,
		"Yes",
		title,
		submission.Description.Title,
		"Yes",
		getContributors(submission),
		submission.Description.Institution.Department,
		"Text",
		genre,
		year.Format("2006"),
		submission.EmbargoDate(),
		language,
		"application/pdf",
		"born digital",
		"<p>" + strings.Join(submission.Content.Abstract.Paragraphs, "</p><p>") + "</p>",
		strings.Join(subjects, " ; "),
		strings.Join(keywords, " ; "),
		"IN COPYRIGHT",
		strings.Join(supplementaryFiles, " ; "),
	}

	// Write row to CSV
	if err := writer.Write(row); err != nil {
		return fmt.Errorf("failed to write row to CSV: %w", err)
	}
	writer.Flush()

	return nil
}

func getContributors(submission proquest.DISSSubmission) string {
	proquestAuthor := submission.Authorship.Authors[0]
	name := "relators:cre:person:"
	name = name + proquestAuthor.Name.Surname
	name = name + ", " + proquestAuthor.Name.First

	author := islandora.Contributor{
		Name:        name,
		Orcid:       proquestAuthor.ORCiD,
		Institution: submission.Description.Institution.Name,
		Status:      "Graduate Student",
	}

	for _, c := range proquestAuthor.Contacts {
		if c.Email != "" {
			author.Email = c.Email
			break
		}
	}
	var contributors []string
	authorJson, err := json.Marshal(author)
	if err != nil {
		slog.Error("Unable to marshal author", "author", author, "err", err)
		return ""
	}
	contributors = append(contributors, string(authorJson))

	for _, proquestAdvisor := range submission.Description.Advisors {
		name := "relators:ths:person:"
		name = name + proquestAdvisor.Name.Surname
		name = name + ", " + proquestAdvisor.Name.First
		advisor := islandora.Contributor{
			Name:        name,
			Institution: submission.Description.Institution.Name,
			Status:      "Faculty",
		}
		aj, err := json.Marshal(advisor)
		if err != nil {
			slog.Error("Unable to marshal advisor", "advisor", advisor, "err", err)
			return ""
		}

		contributors = append(contributors, string(aj))
	}

	return strings.Join(contributors, " ; ")
}

func isDirectory(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}
