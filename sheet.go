package cuesheet

// Sheet holds album metadata and the source files containing its tracks.
type Sheet struct {
	Performer  string
	Songwriter string
	Composer   string
	Genre      string
	Title      string
	Catalog    string
	Year       string
	MbDiscID   string
	GnuDiscID  string
	DiscNumber int
	TotalDiscs int
	Comments   []string
	Files      []*File
}

// Artist prefers performer, then composer and songwriter, with an unknown fallback.
func (s *Sheet) Artist() string {
	switch {
	case s.Performer != "":
		return s.Performer
	case s.Composer != "":
		return s.Composer
	case s.Songwriter != "":
		return s.Songwriter
	default:
		return "Unknown Artist"
	}
}

// File groups tracks and disc metadata for one source file referenced by a CUE sheet.
type File struct {
	Name       string
	Type       string
	Tracks     []*SheetTrack
	Comments   []string
	DiscNumber int
	TotalDiscs int
}

// SheetTrack describes a track's metadata, source positions, gaps, and playback flags.
type SheetTrack struct {
	Number     int
	Title      string
	Performer  string
	Songwriter string
	Composer   string
	Genre      string
	DataType   string
	Indexes    []*Index
	Comments   []string
	ISRC       string
	Pregap     *Time
	Postgap    *Time
	file       *File

	// This flag indicates that digital copying
	// of the track is permitted.
	IsDigitalCopyPermitted bool // DCP
	// This flag indicates that the audio track
	// uses four channels (quadraphonic sound)
	// instead of the standard two channels.
	IsFourChannelAudio bool // 4CH
	// This flag is for audio tracks only and
	// indicates that the track has been recorded
	// with pre-emphasis, which is an older
	// recording technique used to improve
	// the signal-to-noise ratio. Playback
	// software should apply the corresponding
	// de-emphasis filter.
	IsPreEmphasisEnabled bool // PRE
	// This flag is related to copy protection,
	// though it is not supported by all recorders
	// and its function is often ignored.
	IsSerialCopyManagementSystem bool // SCMS
}

// StartAtSample locates the track's INDEX 01, excluding any pregap.
func (t *SheetTrack) StartAtSample(sampleRate int) (int, error) {
	for _, i := range t.Indexes {
		if i.Number == 1 {
			return i.Time.SampleOffset(sampleRate), nil
		}
	}
	return 0, errNoIndex01
}

// Index marks a numbered position within a track's source file.
type Index struct {
	Number int
	Time   *Time
}

// Time represents a CUE position in minutes, seconds, and 75 Hz CD frames.
type Time struct {
	Min    int
	Sec    int
	Frames int
}

// SampleOffset locates the CUE time in samples at the supplied rate.
func (t *Time) SampleOffset(sampleRate int) int {
	return (t.Min*60+t.Sec)*sampleRate + sampleRate/75*t.Frames
}

// FramesOffset expresses the CUE time in 75 Hz CD frames.
func (t *Time) FramesOffset() int {
	return (t.Min*60+t.Sec)*75 + t.Frames
}
