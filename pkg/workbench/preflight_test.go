package workbench

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type localInspector struct{}

func (localInspector) Lstat(name string) (fs.FileInfo, error) { return os.Lstat(name) }
func (localInspector) Readable(name string) error {
	file, err := os.Open(name) // #nosec G304 -- test inspector opens its explicit fixture path.
	if err != nil {
		return err
	}
	return file.Close()
}

func TestParseMediaReferences(t *testing.T) {
	t.Parallel()
	input := " \ufeff id , file , supplemental_file ,title\n1,one.tif,\"two.pdf| https://example.org/remote.pdf\",One\n2,,three.pdf,Two\n"
	references, err := ParseMediaReferences(strings.NewReader(input), DefaultMediaColumns())
	if err != nil {
		t.Fatal(err)
	}
	want := []MediaReference{
		{Row: 2, Column: "file", Path: "one.tif"},
		{Row: 2, Column: "supplemental_file", Path: "two.pdf"},
		{Row: 3, Column: "supplemental_file", Path: "three.pdf"},
	}
	if !reflect.DeepEqual(references, want) {
		t.Fatalf("references = %#v, want %#v", references, want)
	}
}

func TestParseMediaReferencesRejectsUnnamedHeader(t *testing.T) {
	t.Parallel()
	if _, err := ParseMediaReferences(strings.NewReader("file, \none.tif,ignored\n"), DefaultMediaColumns()); err == nil {
		t.Fatal("unnamed Workbench CSV column unexpectedly accepted")
	}
}

func TestParseMediaReferencesReportsNormalizedDuplicateHeader(t *testing.T) {
	t.Parallel()
	_, err := ParseMediaReferences(strings.NewReader("file, file \none.tif,two.tif\n"), DefaultMediaColumns())
	if err == nil || !strings.Contains(err.Error(), `column "file" is duplicated`) {
		t.Fatalf("duplicate header error = %v", err)
	}
}

func TestParseMediaReferencesReportsMalformedRowNumber(t *testing.T) {
	t.Parallel()
	_, err := ParseMediaReferences(strings.NewReader("file,title\none.tif\n"), DefaultMediaColumns())
	if err == nil || !strings.Contains(err.Error(), "row 2") {
		t.Fatalf("malformed row error = %v, want row 2", err)
	}
}

func TestInspectMediaReferencesDistinguishesMissingUnreadableAndInvalid(t *testing.T) {
	t.Parallel()
	root := filepath.ToSlash(t.TempDir())
	available := filepath.Join(root, "available.tif")
	unreadable := filepath.Join(root, "unreadable.tif")
	directory := filepath.Join(root, "directory")
	for filename, mode := range map[string]fs.FileMode{available: 0o600, unreadable: 0o000} {
		if err := os.WriteFile(filename, []byte("fixture"), mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}

	references := []MediaReference{
		{Row: 2, Column: "file", Path: "available.tif"},
		{Row: 3, Column: "file", Path: "missing.tif"},
		{Row: 4, Column: "file", Path: "unreadable.tif"},
		{Row: 5, Column: "file", Path: "directory"},
		{Row: 6, Column: "file", Path: "/outside/file.tif"},
	}
	results, err := InspectMediaReferences(references, root, []string{root}, localInspector{})
	if err != nil {
		t.Fatal(err)
	}
	want := []PathStatus{PathAvailable, PathMissing, PathUnreadable, PathInvalid, PathInvalid}
	for index, status := range want {
		if results[index].Status != status {
			t.Errorf("result %d = %s (%v), want %s", index, results[index].Status, results[index].Err, status)
		}
		if status != PathAvailable && results[index].Err == nil {
			t.Errorf("result %d has no diagnostic", index)
		}
	}
}

func TestInspectMediaReferencesCachesDuplicateProbe(t *testing.T) {
	t.Parallel()
	inspector := &countingInspector{info: fakeFileInfo{mode: 0o600}}
	references := []MediaReference{{Row: 2, Column: "file", Path: "/media/a"}, {Row: 3, Column: "file", Path: "/media/a"}}
	results, err := InspectMediaReferences(references, "/media", []string{"/media"}, inspector)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || inspector.reads != 1 {
		t.Fatalf("results=%d reads=%d, want two results and one readability probe", len(results), inspector.reads)
	}
}

func TestInspectMediaReferencesRejectsRelativeEscapeAndWindowsDrive(t *testing.T) {
	t.Parallel()
	inspector := &countingInspector{info: fakeFileInfo{mode: 0o600}}
	references := []MediaReference{{Path: "../outside.tif"}, {Path: `C:\staging\item.tif`}}
	results, err := InspectMediaReferences(references, "/mnt/islandora_staging", []string{"/mnt", "/home"}, inspector)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.Status != PathInvalid || result.Err == nil {
			t.Fatalf("result = %#v, want invalid diagnostic", result)
		}
	}
	if inspector.reads != 0 {
		t.Fatalf("unsafe paths triggered %d reads", inspector.reads)
	}
}

type countingInspector struct {
	info  fs.FileInfo
	reads int
}

func (i *countingInspector) Lstat(name string) (fs.FileInfo, error) {
	if name == "/media" {
		return fakeFileInfo{mode: fs.ModeDir | 0o700}, nil
	}
	return i.info, nil
}
func (i *countingInspector) Readable(string) error {
	i.reads++
	return nil
}

type fakeFileInfo struct{ mode fs.FileMode }

func (f fakeFileInfo) Name() string       { return "fixture" }
func (f fakeFileInfo) Size() int64        { return 1 }
func (f fakeFileInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeFileInfo) Sys() any           { return nil }
