package workbench

import (
	"reflect"
	"strings"
	"testing"
)

func TestReconcileSupplementalMediaMergesBothArtifacts(t *testing.T) {
	t.Parallel()
	create := strings.NewReader(" \ufeff id , title \na,One\nb,Two\n")
	rollback := strings.NewReader("node_id\n# generated\n100\n101\n")
	existing := strings.NewReader("node_id,file\n100,/staging/public.pdf\n100,/staging/public.pdf\n")
	pending := []SupplementalArtifact{
		{
			Name:               "target.pending_supplemental.csv",
			Reader:             strings.NewReader("id,node_id,file,media_use_tid,published\na,,/staging/overflow.pdf,,\n"),
			DefaultMediaUseTID: "151326",
			DefaultPublished:   "1",
		},
		{
			Name:               "target.unpublished_supplemental.csv",
			Reader:             strings.NewReader("id,node_id,file,media_use_tid,published\nb,,/staging/private.pdf,88,0\n"),
			DefaultMediaUseTID: "151326",
			DefaultPublished:   "0",
		},
	}
	rows, err := ReconcileSupplementalMedia(create, rollback, existing, pending)
	if err != nil {
		t.Fatal(err)
	}
	want := []AddMediaRow{
		{NodeID: "100", File: "/staging/public.pdf", Published: "1"},
		{NodeID: "100", File: "/staging/overflow.pdf", MediaUseTID: "151326", Published: "1"},
		{NodeID: "101", File: "/staging/private.pdf", MediaUseTID: "88", Published: "0"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows = %#v, want %#v", rows, want)
	}
}

func TestReadNamedCSVNormalizesHeaderAndPreservesRowNumbers(t *testing.T) {
	t.Parallel()
	file, err := readNamedCSV(strings.NewReader(" \ufeff id , title \n a , One \n"), "create CSV")
	if err != nil {
		t.Fatal(err)
	}
	if len(file.rows) != 1 || file.rows[0].number != 2 {
		t.Fatalf("rows = %#v, want one row numbered 2", file.rows)
	}
	if got := file.rows[0].values; got["id"] != "a" || got["title"] != "One" {
		t.Fatalf("values = %#v", got)
	}
}

func TestReadNamedCSVRejectsInvalidHeaders(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"duplicate": "id, id \na,b\n",
		"empty":     "id, \na,b\n",
	}
	for name, input := range tests {
		name, input := name, input
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := readNamedCSV(strings.NewReader(input), "create CSV"); err == nil {
				t.Fatal("invalid named CSV header unexpectedly accepted")
			}
		})
	}
}

func TestReadNamedCSVReportsMalformedRowNumber(t *testing.T) {
	t.Parallel()
	_, err := readNamedCSV(strings.NewReader("id,title\na\n"), "create CSV")
	if err == nil || !strings.Contains(err.Error(), "create CSV row 2") {
		t.Fatalf("malformed row error = %v, want create CSV row 2", err)
	}
}

func TestReconcileSupplementalMediaRefusesUnknownIDAndConflict(t *testing.T) {
	t.Parallel()
	base := func(pending string) error {
		_, err := ReconcileSupplementalMedia(
			strings.NewReader("id\na\n"),
			strings.NewReader("node_id\n100\n"),
			nil,
			[]SupplementalArtifact{{Name: "pending", Reader: strings.NewReader(pending), DefaultMediaUseTID: "16", DefaultPublished: "1"}},
		)
		return err
	}
	if err := base("id,node_id,file\nmissing,,/a\n"); err == nil || !strings.Contains(err.Error(), "absent from create") {
		t.Fatalf("unknown ID error = %v", err)
	}
	if err := base("id,node_id,file\na,999,/a\n"); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("conflicting ID error = %v", err)
	}
}

func TestReconcileSupplementalMediaRefusesConflictingDuplicate(t *testing.T) {
	t.Parallel()
	_, err := ReconcileSupplementalMedia(
		strings.NewReader("id\na\n"),
		strings.NewReader("node_id\n100\n"),
		strings.NewReader("node_id,file,published\n100,/same,1\n"),
		[]SupplementalArtifact{{Name: "pending", Reader: strings.NewReader("id,node_id,file,media_use_tid,published\na,,/same,16,0\n")}},
	)
	if err == nil || !strings.Contains(err.Error(), "conflicting add_media") {
		t.Fatalf("conflict error = %v", err)
	}
}
