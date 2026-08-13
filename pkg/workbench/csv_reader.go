package workbench

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

type csvReadOptions struct {
	comment          rune
	fieldsPerRecord  int
	trimLeadingSpace bool
}

type csvWithHeader struct {
	reader        *csv.Reader
	header        []string
	columnIndexes map[string]int
	nextRow       int
}

func readCSVWithHeader(r io.Reader, name string, options csvReadOptions) (*csvWithHeader, error) {
	reader := csv.NewReader(r)
	reader.Comment = options.comment
	reader.FieldsPerRecord = options.fieldsPerRecord
	reader.TrimLeadingSpace = options.trimLeadingSpace

	header, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("%s is empty", name)
		}
		return nil, fmt.Errorf("read %s header: %w", name, err)
	}
	if len(header) == 0 {
		return nil, fmt.Errorf("%s header is empty", name)
	}

	columnIndexes := make(map[string]int, len(header))
	for index, raw := range header {
		column := strings.TrimSpace(raw)
		if index == 0 {
			column = strings.TrimSpace(strings.TrimPrefix(column, "\ufeff"))
		}
		if column == "" {
			return nil, fmt.Errorf("%s column %d has an empty name", name, index+1)
		}
		if first, duplicate := columnIndexes[column]; duplicate {
			return nil, fmt.Errorf("%s column %q is duplicated at columns %d and %d", name, column, first+1, index+1)
		}
		header[index] = column
		columnIndexes[column] = index
	}

	return &csvWithHeader{
		reader:        reader,
		header:        header,
		columnIndexes: columnIndexes,
		nextRow:       2,
	}, nil
}

func (r *csvWithHeader) readRecord() ([]string, int, error) {
	record, err := r.reader.Read()
	if err == io.EOF {
		return nil, 0, io.EOF
	}
	rowNumber := r.nextRow
	r.nextRow++
	return record, rowNumber, err
}
