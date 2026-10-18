package cuesheet

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

//nolint:cyclop // TODO: tech debt
func TestParseSheetBasic(t *testing.T) {
	raw := `
; ignored comment
REM GENRE "Test Genre"
REM DATE 2001-05-01
REM TOTALDISCS 2
REM DISCNUMBER 1
PERFORMER "Demo Band"
TITLE "Demo Album"
COMPOSER "Album Composer"
SONGWRITER "Album Writer"
CATALOG 1234567890123
DISCID TESTDISC01
COMMENT "sheet comment"
FILE "disc-one-demo.flac" WAVE
  COMMENT "file comment"
  TRACK 01 AUDIO
    TITLE "Alpha Song"
    PERFORMER "Track Performer"
    SONGWRITER "Track Writer"
    COMPOSER "Track Composer"
    GENRE "Track Genre"
    INDEX 01 00:00:00
  TRACK 02 AUDIO
    TITLE "Beta Song"
    FLAGS DCP 4CH PRE SCMS
    ISRC TEST123456789
    PREGAP 00:00:32
    POSTGAP 00:01:00
    COMMENT "track comment"
    REM NOTE extra track rem comment
    INDEX 00 04:00:00
    INDEX 01 04:02:00
FILE "disc-two-demo.mp3" MP3
  REM DISCNUMBER 2
  REM TOTALDISCS 2
  TRACK 01 AUDIO
    TITLE "Gamma Song"
    PERFORMER "Second Disc Performer"
    INDEX 01 00:00:00
  TRACK 02 AUDIO
    TITLE "Delta Song"
    SONGWRITER "Second Disc Writer"
    INDEX 01 00:02:00
`
	sheet, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("parseSheet error: %v", err)
	}
	if sheet.Performer != "Demo Band" || sheet.Title != "Demo Album" ||
		sheet.Composer != "Album Composer" ||
		sheet.Songwriter != "Album Writer" {
		t.Fatalf(
			"unexpected sheet artist metadata: performer=%q title=%q composer=%q writer=%q",
			sheet.Performer,
			sheet.Title,
			sheet.Composer,
			sheet.Songwriter,
		)
	}
	if sheet.Genre != "Test Genre" || sheet.Year != "2001" || sheet.Catalog != "1234567890123" ||
		sheet.GnuDiscID != "TESTDISC01" {
		t.Fatalf(
			"unexpected sheet meta: genre=%q year=%q catalog=%q discid=%q",
			sheet.Genre,
			sheet.Year,
			sheet.Catalog,
			sheet.GnuDiscID,
		)
	}
	if len(sheet.Comments) != 1 || sheet.Comments[0] != "sheet comment" {
		t.Fatalf("unexpected sheet comments: %#v", sheet.Comments)
	}
	if got := len(sheet.Files); got != 2 {
		t.Fatalf("expected 2 files, got %d", got)
	}
	file1, file2 := sheet.Files[0], sheet.Files[1]
	if file1.Type != "WAVE" || file1.DiscNumber != 1 || file1.TotalDiscs != 2 {
		t.Fatalf("unexpected first file meta: %+v", file1)
	}
	if len(file1.Comments) != 1 || file1.Comments[0] != "file comment" {
		t.Fatalf("unexpected first file comments: %#v", file1.Comments)
	}
	if len(file1.Tracks) != 2 {
		t.Fatalf("expected 2 tracks in first file, got %d", len(file1.Tracks))
	}
	t1, t2 := file1.Tracks[0], file1.Tracks[1]
	if t1.Title != "Alpha Song" || t1.Performer != "Track Performer" ||
		t1.Songwriter != "Track Writer" ||
		t1.Composer != "Track Composer" ||
		t1.Genre != "Track Genre" {
		t.Fatalf("unexpected first track meta: %+v", t1)
	}
	if len(t1.Indexes) != 1 || t1.Indexes[0].Number != 1 || t1.Indexes[0].Time.FramesOffset() != 0 {
		t.Fatalf("unexpected first track indexes: %#v", t1.Indexes)
	}
	if t2.Title != "Beta Song" || !t2.IsDigitalCopyPermitted || !t2.IsFourChannelAudio ||
		!t2.IsPreEmphasisEnabled ||
		!t2.IsSerialCopyManagementSystem {
		t.Fatalf("unexpected flags on second track: %+v", t2)
	}
	if t2.ISRC != "TEST123456789" {
		t.Fatalf("unexpected ISRC: %q", t2.ISRC)
	}
	if t2.Pregap == nil || t2.Pregap.FramesOffset() != 32 || t2.Postgap == nil ||
		t2.Postgap.FramesOffset() != 75 {
		t.Fatalf("unexpected gaps: pregap=%#v postgap=%#v", t2.Pregap, t2.Postgap)
	}
	if len(t2.Indexes) != 2 || t2.Indexes[0].Number != 0 || t2.Indexes[1].Number != 1 {
		t.Fatalf("unexpected second track indexes: %#v", t2.Indexes)
	}
	if len(t2.Comments) != 2 || t2.Comments[0] != "track comment" ||
		t2.Comments[1] != "NOTE extra track rem comment" {
		t.Fatalf("unexpected second track comments: %#v", t2.Comments)
	}
	if file2.Type != "MP3" || file2.DiscNumber != 2 || file2.TotalDiscs != 2 {
		t.Fatalf("unexpected second file meta: %+v", file2)
	}
	if len(file2.Tracks) != 2 {
		t.Fatalf("expected 2 tracks in second file, got %d", len(file2.Tracks))
	}
	if file2.Tracks[0].Title != "Gamma Song" ||
		file2.Tracks[0].Performer != "Second Disc Performer" {
		t.Fatalf("unexpected third track meta: %+v", file2.Tracks[0])
	}
	if file2.Tracks[1].Songwriter != "Second Disc Writer" {
		t.Fatalf("unexpected fourth track songwriter: %+v", file2.Tracks[1])
	}
}

func TestParseSheetErrors(t *testing.T) {
	_, err := Parse([]byte("TRACK 01 AUDIO\nINDEX 01 00:00:00\n"))
	if err == nil {
		t.Fatal("expected error when TRACK comes before FILE")
	}
	_, err = Parse([]byte("FILE \"one.flac\" WAVE\nTRACK XX AUDIO\n"))
	if err == nil {
		t.Fatal("expected error on invalid track number")
	}
	_, err = Parse([]byte("FILE \"one.flac\" WAVE\nTRACK 01 AUDIO\nINDEX 01 not-a-time\n"))
	if err == nil {
		t.Fatal("expected error on invalid time")
	}
}

const oneTrack = "FILE \"one.flac\" WAVE\n  TRACK 01 AUDIO\n    TITLE \"T\"\n    INDEX 01 00:00:00\n"

func TestParseSheetInvalidInputs(t *testing.T) {
	cases := map[string]string{
		"index before track":     "FILE \"a\" WAVE\nINDEX 01 00:00:00\n",
		"index bad number":       "FILE \"a\" WAVE\nTRACK 01 AUDIO\nINDEX XX 00:00:00\n",
		"index missing time":     "FILE \"a\" WAVE\nTRACK 01 AUDIO\nINDEX 01\n",
		"time wrong parts":       "FILE \"a\" WAVE\nTRACK 01 AUDIO\nINDEX 01 00:00\n",
		"time bad minutes":       "FILE \"a\" WAVE\nTRACK 01 AUDIO\nINDEX 01 xx:00:00\n",
		"time bad seconds":       "FILE \"a\" WAVE\nTRACK 01 AUDIO\nINDEX 01 00:xx:00\n",
		"time bad frames":        "FILE \"a\" WAVE\nTRACK 01 AUDIO\nINDEX 01 00:00:xx\n",
		"title without value":    "TITLE\n",
		"file without type":      "FILE \"a.flac\"\n",
		"track without type":     "FILE \"a\" WAVE\nTRACK 01\n",
		"pregap before track":    "PREGAP 00:00:10\n",
		"pregap without time":    "FILE \"a\" WAVE\nTRACK 01 AUDIO\nPREGAP\n",
		"postgap bad time":       "FILE \"a\" WAVE\nTRACK 01 AUDIO\nPOSTGAP nope\n",
		"bad discnumber":         "DISCNUMBER one\n",
		"bad totaldiscs":         "TOTALDISCS many\n",
		"audio without index 01": "FILE \"a\" WAVE\nTRACK 01 AUDIO\nINDEX 00 00:00:00\n",
		"scanner token too long": "REM X " + strings.Repeat("a", 70000) + "\n",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(raw))
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}
}

func TestParseSheetTolerance(t *testing.T) {
	raw := "\ufeffrem\n\"\nREM DISCNUMBER abc\nREM TOTALDISCS abc\nREM\tCUSTOM some value\n" +
		"UNKNOWN directive\nISRC ORPHAN\nFLAGS DCP\nFLAGS\n" +
		"date 99\n" +
		"file \"a.flac\" wave\n" +
		"  track 01 audio\n" +
		"    title \"\"\n" +
		"    flags dcp unknown pre\n" +
		"    index 01 01:02:03\n"
	sheet, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sheet.Year != "99" {
		t.Errorf("short date should be kept as is, got %q", sheet.Year)
	}
	if !reflect.DeepEqual(sheet.Comments, []string{"CUSTOM some value"}) {
		t.Errorf("unexpected sheet comments: %#v", sheet.Comments)
	}
	file := sheet.Files[0]
	if file.Type != "WAVE" {
		t.Errorf("file type should be upper-cased, got %q", file.Type)
	}
	track := file.Tracks[0]
	if track.DataType != "AUDIO" || !track.IsDigitalCopyPermitted || !track.IsPreEmphasisEnabled ||
		track.IsFourChannelAudio || track.IsSerialCopyManagementSystem {
		t.Errorf("unexpected track: %+v", track)
	}
	if track.ISRC != "" {
		t.Errorf("ISRC before TRACK must be ignored, got %q", track.ISRC)
	}
	// empty quoted title falls back to the file name
	if track.Title != "a" {
		t.Errorf("expected title inferred from file name, got %q", track.Title)
	}
	got, err := track.StartAtSample(44100)
	if err != nil || got != (62*44100+588*3) {
		t.Errorf("unexpected start: %d, %v", got, err)
	}
}

func TestParseSheetDiscScope(t *testing.T) {
	raw := "DISCNUMBER 3\nTOTALDISCS 4\n" +
		"FILE \"a.flac\" WAVE\n  TRACK 01 AUDIO\n    TITLE \"A\"\n    INDEX 01 00:00:00\n" +
		"FILE \"b.flac\" WAVE\n  TRACK 02 AUDIO\n    TITLE \"B\"\n    INDEX 01 00:00:00\n"
	sheet, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if sheet.DiscNumber != 3 || sheet.TotalDiscs != 4 {
		t.Errorf("unexpected sheet discs: %d/%d", sheet.DiscNumber, sheet.TotalDiscs)
	}
	if f := sheet.Files[0]; f.DiscNumber != 3 || f.TotalDiscs != 4 {
		t.Errorf("first file should inherit discs: %+v", f)
	}
	if f := sheet.Files[1]; f.DiscNumber != 0 || f.TotalDiscs != 0 {
		t.Errorf("second file must not inherit discs: %+v", f)
	}
}

func TestParseSheetLazyTitles(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		wantAlbum  string
		wantTitles []string
	}{
		{
			"plain file names",
			"FILE \"01 - First.flac\" WAVE\n  TRACK 01 AUDIO\n    INDEX 01 00:00:00\n" +
				"FILE \"02 - Second.flac\" WAVE\n  TRACK 02 AUDIO\n    INDEX 01 00:00:00\n",
			"Unknown Album",
			[]string{"01 - First", "02 - Second"},
		},
		{
			"directory in file name becomes album",
			"FILE \"Band\\\\Album\\\\01 Song.flac\" WAVE\n  TRACK 01 AUDIO\n    INDEX 01 00:00:00\n",
			"Band/Album",
			[]string{"01 Song"},
		},
		{
			"directory kept when album is titled",
			"TITLE \"Given\"\nFILE \"Band/Album/01 Song.flac\" WAVE\n" +
				"  TRACK 01 AUDIO\n    INDEX 01 00:00:00\n",
			"Given",
			[]string{"Band/Album/01 Song"},
		},
	}
	for _, tc := range cases {
		sheet, err := Parse([]byte(tc.raw))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if sheet.Title != tc.wantAlbum {
			t.Errorf("%s: want album %q, got %q", tc.name, tc.wantAlbum, sheet.Title)
		}
		for i, want := range tc.wantTitles {
			if got := sheet.Files[i].Tracks[0].Title; got != want {
				t.Errorf("%s: want track title %q, got %q", tc.name, want, got)
			}
		}
	}
}

func TestParseSheetPerformerInference(t *testing.T) {
	build := func(performers ...string) string {
		lines := []string{"FILE \"a.flac\" WAVE"}
		for i, p := range performers {
			lines = append(lines, "TRACK 0"+string(rune('1'+i))+" AUDIO", "TITLE \"T\"")
			if p != "" {
				lines = append(lines, "PERFORMER \""+p+"\"")
			}
			lines = append(lines, "INDEX 01 00:00:00")
		}
		return strings.Join(lines, "\n") + "\n"
	}
	cases := []struct {
		name       string
		performers []string
		want       string
	}{
		{"single", []string{"Solo", ""}, "Solo"},
		{"all different", []string{"A", "B"}, "Various Artists"},
		{"majority", []string{"A", "A", "B", ""}, "A"},
		{"none", []string{"", ""}, "Unknown Artist"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sheet, err := Parse([]byte(build(tc.performers...)))
			if err != nil {
				t.Fatal(err)
			}
			if sheet.Performer != tc.want {
				t.Errorf("want %q, got %q", tc.want, sheet.Performer)
			}
		})
	}
}

func TestParseSheetNonAudioAndZeroNumber(t *testing.T) {
	raw := "FILE \"a.bin\" BINARY\n" +
		"TRACK 00 MODE1/2352\nTITLE \"Data\"\n" +
		"TRACK 02 AUDIO\nTITLE \"Song\"\nINDEX 01 00:00:00\n"
	sheet, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(sheet.Files[0].Tracks) != 1 || sheet.Files[0].Tracks[0].Number != 2 {
		t.Errorf("unexpected tracks: %+v", sheet.Files[0].Tracks)
	}
}

func TestTokenize(t *testing.T) {
	cp := &cueParser{}
	got := cp.tokenize(`A "" B "x  y"  "z"w`)
	want := []string{"A", "", "B", "x  y", "zw"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %#v, got %#v", want, got)
	}
}

func TestSheetHelpers(t *testing.T) {
	artists := []struct {
		sheet Sheet
		want  string
	}{
		{Sheet{Performer: "P", Composer: "C", Songwriter: "S"}, "P"},
		{Sheet{Composer: "C", Songwriter: "S"}, "C"},
		{Sheet{Songwriter: "S"}, "S"},
		{Sheet{}, "Unknown Artist"},
	}
	for _, tc := range artists {
		if got := tc.sheet.Artist(); got != tc.want {
			t.Errorf("want %q, got %q", tc.want, got)
		}
	}
	tm := &Time{Min: 1, Sec: 2, Frames: 3}
	if got := tm.SampleOffset(44100); got != 62*44100+588*3 {
		t.Errorf("unexpected sample offset %d", got)
	}
	track := &SheetTrack{Indexes: []*Index{{Number: 0, Time: tm}}}
	_, err := track.StartAtSample(44100)
	if !errors.Is(err, errNoIndex01) {
		t.Errorf("expected errNoIndex01, got %v", err)
	}
	_, err = Parse([]byte(oneTrack))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
