package workbench

import (
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestReadCSVWithHeaderNormalizesHeaderAndTracksRows(t *testing.T) {
	t.Parallel()

	file, err := readCSVWithHeader(strings.NewReader(" \ufeff id , file \nitem-1, image.tif \n"), "test CSV", csvReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"id", "file"}; !reflect.DeepEqual(file.header, want) {
		t.Fatalf("header = %#v, want %#v", file.header, want)
	}
	if file.columnIndexes["id"] != 0 || file.columnIndexes["file"] != 1 {
		t.Fatalf("column indexes = %#v", file.columnIndexes)
	}

	record, rowNumber, err := file.readRecord()
	if err != nil {
		t.Fatal(err)
	}
	if rowNumber != 2 || !reflect.DeepEqual(record, []string{"item-1", " image.tif "}) {
		t.Fatalf("record = %#v at row %d", record, rowNumber)
	}
	if _, _, err := file.readRecord(); err != io.EOF {
		t.Fatalf("read trailing record error = %v, want EOF", err)
	}
}

func TestReadCSVWithHeaderRejectsInvalidHeaders(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"duplicate": " id ,id\n",
		"empty":     "id, \n",
		"no header": "",
	}
	for name, input := range tests {
		name, input := name, input
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := readCSVWithHeader(strings.NewReader(input), "test CSV", csvReadOptions{}); err == nil {
				t.Fatal("invalid header unexpectedly accepted")
			}
		})
	}
}

func TestReadCSVWithHeaderDoesNotStripEmbeddedBOM(t *testing.T) {
	t.Parallel()

	file, err := readCSVWithHeader(strings.NewReader("id,\ufefffile\nitem-1,image.tif\n"), "test CSV", csvReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if file.header[1] != "\ufefffile" {
		t.Fatalf("second header = %q, want embedded BOM preserved", file.header[1])
	}
	if _, ok := file.columnIndexes["file"]; ok {
		t.Fatal("embedded BOM column was silently treated as file")
	}
}
